// internal/backend/echo.go
//
// Toy protected resource sitting behind the rate limiter. Deliberately
// uninteresting per the problem statement -- its only job is to prove a
// request made it all the way through auth -> rate limiter -> logging and
// got forwarded, by echoing back what it received. No business logic
// belongs here; if you're tempted to add some, it belongs in a different
// project.
package backend

import (
	"encoding/json"
	"net/http"
	"time"

	"ratelimiter/internal/middleware"
)

// echoResponse is the JSON body returned for every request. Keeping it
// small and flat makes it trivial to confirm "this was actually a 200
// from the backend" vs. a 429 from the rate limiter, without needing to
// parse anything interesting out of it.
type echoResponse struct {
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	ClientID  string            `json:"client_id,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Timestamp string            `json:"timestamp"`
}

// Handler returns an http.HandlerFunc that echoes the request back as
// JSON with a 200. It reads the client ID off the request context via
// middleware.ClientIDFromContext so the response shows which identity the
// auth middleware resolved this request to -- useful for confirming
// during manual testing that two different API keys really do get
// tracked as separate rate-limit buckets.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		headers := make(map[string]string, len(r.Header))
		for k := range r.Header {
			headers[k] = r.Header.Get(k)
		}

		resp := echoResponse{
			Method:    r.Method,
			Path:      r.URL.Path,
			ClientID:  middleware.ClientIDFromContext(r.Context()),
			Headers:   headers,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}