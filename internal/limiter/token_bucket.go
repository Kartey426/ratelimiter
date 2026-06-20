// internal/limiter/token_bucket.go
//
// Token bucket implementation. All state lives in Redis; the check+consume
// is done inside lua/token_bucket.lua so concurrent callers can never both
// read the same token count before either writes back (the race condition
// described in the problem statement).
package limiter
