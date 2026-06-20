// internal/middleware/logging.go
//
// Minimal request logger sitting after the rate limiter: method, path,
// client ID, allowed/denied, latency. Feeds /admin/stats and the
// load-test's after-the-fact verification.
package middleware

import (
	"log"
	"net/http"
	"sync"
	"time"
)

// statusRecorder wraps http.ResponseWriter so this middleware can observe
// the status code RateLimit (or any earlier handler) ultimately wrote,
// without RateLimit needing to know logging exists. http.ResponseWriter
// has no built-in way to read back the status after WriteHeader is
// called, so capturing it requires this small wrapper.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// clientStats holds the running allowed/denied counts for one client ID.
// Kept intentionally tiny -- this is a demo/debug counter, not a metrics
// system.
type clientStats struct {
	Allowed int
	Denied  int
}

// statsMu guards statsByClient. A plain mutex is fine here: this is a
// toy gateway's debug endpoint, not a hot path that needs lock-free
// counters.
var (
	statsMu       sync.Mutex
	statsByClient = make(map[string]*clientStats)
)

// Logging returns middleware that logs one line per request -- method,
// path, client ID, allowed/denied, latency -- after the rest of the
// chain (notably RateLimit) has already decided the outcome. It must be
// placed AFTER RateLimit in the chain (i.e. RateLimit wraps Logging, or
// equivalently Logging is the innermost middleware before the backend)
// so the status code it observes reflects the real decision, not a
// placeholder.
//
// It also updates an in-process per-client allowed/denied counter,
// exposed via StatsSnapshot for internal/stats to read from when
// building GET /admin/stats -- this file doesn't import internal/stats
// itself (avoiding a dependency in the wrong direction); stats.go is
// expected to call StatsSnapshot() instead.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		clientID := ClientIDFromContext(r.Context())

		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sr, r)

		latency := time.Since(start)
		allowed := sr.status != http.StatusTooManyRequests

		recordResult(clientID, allowed)

		log.Printf(
			"method=%s path=%s client=%s status=%d allowed=%t latency=%s",
			r.Method, r.URL.Path, clientID, sr.status, allowed, latency,
		)
	})
}

// recordResult updates the in-process counters for clientID. Unexported:
// the only supported read path is StatsSnapshot, so callers can't get
// into an inconsistent state by poking statsByClient directly.
func recordResult(clientID string, allowed bool) {
	statsMu.Lock()
	defer statsMu.Unlock()

	s, ok := statsByClient[clientID]
	if !ok {
		s = &clientStats{}
		statsByClient[clientID] = s
	}
	if allowed {
		s.Allowed++
	} else {
		s.Denied++
	}
}

// ClientStatsSnapshot is a point-in-time copy of one client's counters,
// safe to hand out without holding the internal lock.
type ClientStatsSnapshot struct {
	ClientID string
	Allowed  int
	Denied   int
}

// StatsSnapshot returns a copy of current per-client allowed/denied
// counts. Intended to be called from internal/stats when building the
// GET /admin/stats response. Returns a fresh slice/copy each call so the
// caller can't mutate this package's internal state.
func StatsSnapshot() []ClientStatsSnapshot {
	statsMu.Lock()
	defer statsMu.Unlock()

	out := make([]ClientStatsSnapshot, 0, len(statsByClient))
	for clientID, s := range statsByClient {
		out = append(out, ClientStatsSnapshot{
			ClientID: clientID,
			Allowed:  s.Allowed,
			Denied:   s.Denied,
		})
	}
	return out
}