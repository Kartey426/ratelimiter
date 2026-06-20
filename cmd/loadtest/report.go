// cmd/loadtest/report.go
//
// Aggregation + the correctness assertion itself: groups results into
// per-second buckets and checks allowed-per-bucket never exceeds the
// configured limit. Also computes the burst/smoothing comparison numbers
// (e.g. max burst size allowed, variance over time) referenced in the
// problem statement's "real numbers" requirement.
package main
