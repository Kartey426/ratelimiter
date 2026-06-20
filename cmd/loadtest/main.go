// cmd/loadtest/main.go
//
// The actual deliverable (see problem statement: "the demonstration that
// it holds under concurrency is the deliverable, not the algorithm").
//
// Spins up N goroutines per client, each firing requests as fast as
// possible against a round-robin selection of target gateway URLs (so
// traffic actually splits across multiple instances sharing one Redis),
// and reports whether the configured limit was EVER exceeded for any
// client -- the actual correctness claim.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	var (
		targets     = flag.String("targets", "http://localhost:8081,http://localhost:8082", "comma-separated gateway base URLs")
		clients     = flag.Int("clients", 5, "number of distinct simulated clients (API keys)")
		concurrency = flag.Int("concurrency", 20, "concurrent goroutines per client")
		duration    = flag.Duration("duration", 10*time.Second, "how long to hammer the gateway")
		algoTag     = flag.String("algo", "unspecified", "label for this run, e.g. token_bucket or sliding_window (for the report filename only -- does not change gateway behavior)")
		path        = flag.String("path", "/api/loadtest", "path to hit on each gateway")
	)
	flag.Parse()

	targetList := strings.Split(*targets, ",")
	for i := range targetList {
		targetList[i] = strings.TrimSpace(targetList[i])
	}

	fmt.Printf("load test starting: targets=%v clients=%d concurrency_per_client=%d duration=%s algo_tag=%s\n",
		targetList, *clients, *concurrency, *duration, *algoTag)

	results := runLoadTest(loadTestConfig{
		targets:     targetList,
		numClients:  *clients,
		concurrency: *concurrency,
		duration:    *duration,
		path:        *path,
	})

	report := BuildReport(*algoTag, results)
	report.Print()

	if err := report.WriteJSON("results"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write results JSON: %v\n", err)
	}
}

// loadTestConfig bundles the knobs for one run.
type loadTestConfig struct {
	targets     []string
	numClients  int
	concurrency int
	duration    time.Duration
	path        string
}

// requestResult is one HTTP call's outcome, with just enough detail for
// the correctness check, the per-client/per-second burst comparison, and
// the per-target breakdown that proves traffic actually got split across
// multiple gateway instances rather than all landing on one.
type requestResult struct {
	ClientID   string
	Target     string
	Allowed    bool
	StatusCode int
	Timestamp  time.Time
}

// runLoadTest fires concurrent requests for the configured duration and
// returns every individual result. Each simulated client gets its own
// API key (client-0, client-1, ...) so per-client isolation is also
// exercised, not just one shared bucket. Within a client, `concurrency`
// goroutines hammer round-robin across all targets simultaneously --
// this is what actually proves cross-instance enforcement, since two
// goroutines for the SAME client can land on DIFFERENT gateway ports in
// the same instant.
func runLoadTest(cfg loadTestConfig) []requestResult {
	var (
		mu      sync.Mutex
		results []requestResult
		wg      sync.WaitGroup
	)

	httpClient := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(cfg.duration)

	var targetCounter uint64 // round-robins across targets across all goroutines

	for c := 0; c < cfg.numClients; c++ {
		clientID := fmt.Sprintf("client-%d", c)

		for g := 0; g < cfg.concurrency; g++ {
			wg.Add(1)
			go func(clientID string) {
				defer wg.Done()
				for time.Now().Before(deadline) {
					idx := atomic.AddUint64(&targetCounter, 1)
					target := cfg.targets[idx%uint64(len(cfg.targets))]

					status := doRequest(httpClient, target, cfg.path, clientID)

					mu.Lock()
					results = append(results, requestResult{
						ClientID:   clientID,
						Target:     target,
						Allowed:    status == http.StatusOK,
						StatusCode: status,
						Timestamp:  time.Now(),
					})
					mu.Unlock()
				}
			}(clientID)
		}
	}

	wg.Wait()
	return results
}

// doRequest fires one request and returns the HTTP status code, or 0 if
// the request itself failed (connection refused, timeout, etc -- treated
// as neither allowed nor denied, just unreachable).
func doRequest(client *http.Client, baseURL, path, clientID string) int {
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("X-API-Key", clientID)

	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	return resp.StatusCode
}