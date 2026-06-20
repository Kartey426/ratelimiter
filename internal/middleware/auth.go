package middleware

import (
	"context"
	"net/http"
)

// ctxKey is an unexported type so this package's context keys can never
// collide with keys set by other packages (the standard Go idiom for
// context values).
type ctxKey int
const clientIDKey ctxKey = iota
const APIKeyHeader = "X-API-Key"
const anonymousClientID = "anonymous"

func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID := r.Header.Get(APIKeyHeader)
		if clientID == "" {
			clientID = r.URL.Query().Get("api_key")
		}
		if clientID == "" {
			clientID = anonymousClientID
		}

		ctx := context.WithValue(r.Context(), clientIDKey, clientID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func ClientIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(clientIDKey).(string); ok && id != "" {
		return id
	}
	return anonymousClientID
}