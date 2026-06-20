// cmd/loadtest/main.go
//
// The actual deliverable (see problem statement: "the demonstration that
// it holds under concurrency is the deliverable, not the algorithm").
//
// Responsibilities:
//   1. Spin up N goroutines, each a simulated client firing requests as
//      fast as possible (configurable: concurrency, total requests, target
//      rate).
//   2. Round-robin or split traffic across MULTIPLE gateway instance URLs
//      (e.g. :8081, :8082) hitting the same Redis, to prove the limit is
//      global, not per-instance.
//   3. Collect results: total sent, total allowed, total denied, and the
//      critical correctness check -- did allowed count EVER exceed the
//      configured limit in any window. This must be zero, always.
//   4. Run once per algorithm (token bucket vs sliding window) and diff
//      the burst/smoothing behavior with real numbers.
//   5. Write results to ../../results/<algorithm>-<timestamp>.json (and/or
//      print a summary table to stdout) for the README chart.
package main

func main() {
	// TODO: flag parsing (concurrency, duration, target URLs, algorithm tag)
	// TODO: worker pool of goroutines hammering the gateway
	// TODO: aggregate + verify + report
}
