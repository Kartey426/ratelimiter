// internal/middleware/ratelimit.go
//
// HTTP middleware that wraps a limiter.Limiter. On deny, writes 429 with
// Retry-After (seconds, rounded up, derived from the Decision returned by
// the limiter -- never a hardcoded value, so it stays honest). On allow,
// sets X-RateLimit-Limit / X-RateLimit-Remaining and calls next.
package middleware

import(
	"ratelimiter/internal/limiter"
	"math"
	"net/http"
	"strconv"
	"log"
)
func RateLimit(lim limiter.Limiter, limitForHeader int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientID := ClientIDFromContext(r.Context())

			decision, err := lim.Allow(r.Context(), clientID)
			if err != nil {
				log.Printf("rate limiter error for client %s: %v", clientID, err)
				http.Error(w, "rate limiter unavailable", http.StatusInternalServerError)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limitForHeader))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))

			if !decision.Allowed {
				retrySeconds := int(math.Ceil(decision.RetryAfter.Seconds()))
				w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}