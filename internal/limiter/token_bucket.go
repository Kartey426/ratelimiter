// internal/limiter/token_bucket.go
//
// Token bucket implementation. All state lives in Redis; the check+consume
// is done inside lua/token_bucket.lua so concurrent callers can never both
// read the same token count before either writes back (the race condition
// described in the problem statement).
package limiter

import(
	_ "embed"
	"context"
	"time"
	"fmt"
	"github.com/redis/go-redis/v9"
)

//go:embed lua/token_bucket.lua
var tokenBucketScriptSrc string

type TokenBucketConfig struct{
	Capacity float64
	RefillRate float64
	TTL time.Duration
	KeyPrefix string
}

type TokenBucket struct{
	rdb redis.Cmdable
	script *redis.Script
	cfg TokenBucketConfig
}

func NewTokenBucket(rdb redis.Cmdable, cfg TokenBucketConfig) *TokenBucket{
	if cfg.KeyPrefix==""{
		cfg.KeyPrefix="tb"
	}
	if cfg.TTL<=0{
		// Comfortably longer than a full empty->full refill cycle.
		fullRefillSeconds := cfg.Capacity / cfg.RefillRate
		cfg.TTL = time.Duration(fullRefillSeconds*2+60) * time.Second
	}
	return &TokenBucket{
		rdb: rdb,
		script: redis.NewScript(tokenBucketScriptSrc),
		cfg: cfg,
	}
}

func (tb *TokenBucket) Allow(ctx context.Context, clientID string) (Decision, error){
	key := fmt.Sprintf("%s:{%s}", tb.cfg.KeyPrefix, clientID)
	nowMs := time.Now().UnixMilli()

	res, err := tb.script.Run(ctx, tb.rdb, []string{key},
		tb.cfg.Capacity,
		tb.cfg.RefillRate,
		nowMs,
		1, // requested tokens
		int(tb.cfg.TTL.Seconds()),
	).Result()
	if err != nil {
		return Decision{}, fmt.Errorf("token bucket allow: %w", err)
	}

	allowed, remaining, retryAfterMs, err := parseDecisionReply(res)
	if err != nil {
		return Decision{}, fmt.Errorf("token bucket allow: %w", err)
	}

	return Decision{
		Allowed:    allowed,
		Remaining:  remaining,
		RetryAfter: time.Duration(retryAfterMs) * time.Millisecond,
	}, nil
}