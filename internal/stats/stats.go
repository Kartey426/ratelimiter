// internal/stats/stats.go
//
// Backs GET /admin/stats. Tracks per-client allowed/denied counts (reads
// from Redis or an in-process counter fed by the logging middleware) for
// live demo/debugging, separate from the load tester's own report.
package stats
