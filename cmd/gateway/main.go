// cmd/gateway/main.go
//
// Entry point for one gateway instance. Reads PORT + REDIS_ADDR from env
// (see internal/config), wires up:
//   auth middleware -> rate limiter middleware -> logging -> backend handler
// Run two of these on different ports against the same Redis to prove the
// "distributed, not per-instance" claim.
package main
import(
	"ratelimiter/internal/config"
	"ratelimiter/internal/middleware"
	"ratelimiter/internal/backend"
	"ratelimiter/internal/limiter"
	"github.com/redis/go-redis/v9"
	"time"
	"net/http"
	"context"
	"log"
)
func main() {
	// TODO: load config, connect redis, build mux, http.ListenAndServe
	cfg, err:= config.Load()
	if err!=nil{
		log.Fatalf("config: %v",err)
	}
	rdb:=redis.NewClient(&redis.Options{
		Addr:cfg.RedisAddr,
	})
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err:=rdb.Ping(pingCtx).Err();err!=nil{
		log.Fatalf("redis cannot be connected to %s: %v",cfg.RedisAddr,err)
	}
	lim:=buildLimiter(rdb,cfg)
	log.Printf("rate limiter: algorithm: %s ; limit: %d", cfg.Algorithm, cfg.Limit)
	mux:=http.NewServeMux()
	protected := middleware.Auth(
		middleware.RateLimit(lim, cfg.LimitForHeader())(
			middleware.Logging(
				backend.Handler(),
			),
		),
	)
	mux.Handle("/api/",protected)
	addr:= ":" + cfg.Port
	log.Printf("gateway listening on %s (redis = %s)", addr, cfg.RedisAddr)
	if err:=http.ListenAndServe(addr, mux);err!=nil{
		log.Fatalf("server: %v", err)
	}
}

func buildLimiter(rdb redis.Cmdable, cfg config.Config) limiter.Limiter{
	switch cfg.Algorithm{
	case "token_bucket":
		return limiter.NewTokenBucket(rdb, limiter.TokenBucketConfig{
			Capacity: cfg.Capacity,
			RefillRate: cfg.RefillRate,
		})
	case "sliding_window":
		return limiter.NewSlidingWindow(rdb, limiter.SlidingWindowConfig{
			Limit: cfg.Limit,
			WindowSize: cfg.WindowDuration(),
	})
	default:
		log.Fatalf("buildLimiter: unknown algorithm %q", cfg.Algorithm)
	return nil
	}
}