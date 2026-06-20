// internal/middleware/logging.go
//
// Minimal request logger sitting after the rate limiter: method, path,
// client ID, allowed/denied, latency. Feeds /admin/stats and the
// load-test's after-the-fact verification.
package middleware
