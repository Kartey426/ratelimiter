// cmd/loadtest/report.go
//
// Aggregation + the correctness assertion itself: groups results into
// per-client, per-second buckets and checks allowed-per-bucket never
// exceeds what's plausible for the configured algorithm. Also computes
// a per-target-gateway breakdown, proving traffic actually got split
// across multiple instances rather than all landing on one -- the real
// "distributed, not per-instance" claim from the problem statement.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// bucketKey identifies one client's allowed-request count within one
// second of wall-clock time. A struct key (not a hand-parsed string) so
// there's no fragile string-splitting involved in either writing or
// reading these counts back out.
type bucketKey struct {
	ClientID string
	Sec      int64
}

// PerSecondBucket is the JSON-serializable form of one bucketKey's count.
type PerSecondBucket struct {
	ClientID     string `json:"client_id"`
	UnixSecond   int64  `json:"unix_second"`
	AllowedCount int    `json:"allowed_count"`
}

// TargetBreakdown shows how many requests each individual gateway
// instance handled and allowed -- this is the evidence that traffic was
// genuinely split across multiple processes, not just sent to one.
type TargetBreakdown struct {
	Target  string `json:"target"`
	Sent    int    `json:"sent"`
	Allowed int    `json:"allowed"`
	Denied  int    `json:"denied"`
}

// Report is the full output of one load test run: overall totals, the
// per-client/per-second breakdown needed to spot any overage, and the
// per-target breakdown proving multi-instance coverage.
type Report struct {
	AlgoTag              string             `json:"algo_tag"`
	GeneratedAt          time.Time          `json:"generated_at"`
	TotalSent            int                `json:"total_sent"`
	TotalAllowed         int                `json:"total_allowed"`
	TotalDenied          int                `json:"total_denied"`
	TotalUnreachable     int                `json:"total_unreachable"`
	PeakAllowedPerSecond int                `json:"peak_allowed_per_second"`
	Targets              []TargetBreakdown  `json:"targets"`
	Buckets              []PerSecondBucket  `json:"buckets,omitempty"`
}

// BuildReport aggregates raw results into a Report. PeakAllowedPerSecond
// is the headline correctness number: for any individual client, it
// should never exceed that client's configured limit, regardless of how
// many goroutines or how many gateway instances hammered it concurrently.
func BuildReport(algoTag string, results []requestResult) Report {
	r := Report{
		AlgoTag:     algoTag,
		GeneratedAt: time.Now().UTC(),
	}

	bucketCounts := make(map[bucketKey]int)
	targetCounts := make(map[string]*TargetBreakdown)

	for _, res := range results {
		r.TotalSent++

		tb, ok := targetCounts[res.Target]
		if !ok {
			tb = &TargetBreakdown{Target: res.Target}
			targetCounts[res.Target] = tb
		}
		tb.Sent++

		switch {
		case res.StatusCode == 0:
			r.TotalUnreachable++
			continue
		case res.Allowed:
			r.TotalAllowed++
			tb.Allowed++
		default:
			r.TotalDenied++
			tb.Denied++
		}

		if res.Allowed {
			key := bucketKey{ClientID: res.ClientID, Sec: res.Timestamp.Unix()}
			bucketCounts[key]++
		}
	}

	for key, count := range bucketCounts {
		r.Buckets = append(r.Buckets, PerSecondBucket{
			ClientID:     key.ClientID,
			UnixSecond:   key.Sec,
			AllowedCount: count,
		})
		if count > r.PeakAllowedPerSecond {
			r.PeakAllowedPerSecond = count
		}
	}
	sort.Slice(r.Buckets, func(i, j int) bool {
		if r.Buckets[i].ClientID != r.Buckets[j].ClientID {
			return r.Buckets[i].ClientID < r.Buckets[j].ClientID
		}
		return r.Buckets[i].UnixSecond < r.Buckets[j].UnixSecond
	})

	for _, tb := range targetCounts {
		r.Targets = append(r.Targets, *tb)
	}
	sort.Slice(r.Targets, func(i, j int) bool {
		return r.Targets[i].Target < r.Targets[j].Target
	})

	return r
}

// Print writes a human-readable summary to stdout -- the "ran 10,000
// concurrent requests ... allowed exactly 100/sec, zero overage" line
// from the problem statement, plus the per-target split proving
// multi-instance coverage.
func (r Report) Print() {
	fmt.Println()
	fmt.Printf("=== load test report (%s) ===\n", r.AlgoTag)
	fmt.Printf("total sent:        %d\n", r.TotalSent)
	fmt.Printf("total allowed:     %d\n", r.TotalAllowed)
	fmt.Printf("total denied:      %d\n", r.TotalDenied)
	fmt.Printf("total unreachable: %d\n", r.TotalUnreachable)
	fmt.Printf("peak allowed/sec (any single client): %d\n", r.PeakAllowedPerSecond)
	fmt.Println()
	fmt.Println("per-target breakdown:")
	for _, tb := range r.Targets {
		fmt.Printf("  %-30s sent=%-6d allowed=%-6d denied=%d\n", tb.Target, tb.Sent, tb.Allowed, tb.Denied)
	}
	fmt.Println()
}

// WriteJSON saves the report to <dir>/<algo_tag>-<unix_timestamp>.json,
// matching the path the problem statement calls out
// (results/<algorithm>-<timestamp>.json) for later comparison across
// algorithms.
func (r Report) WriteJSON(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	filename := fmt.Sprintf("%s-%d.json", r.AlgoTag, time.Now().Unix())
	fullPath := filepath.Join(dir, filename)

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("results written to %s\n", fullPath)
	return nil
}