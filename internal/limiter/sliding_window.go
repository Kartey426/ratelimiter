// internal/limiter/sliding_window.go
//
// Sliding window counter implementation (weighted average of current +
// previous fixed window). Atomic check+increment lives in
// lua/sliding_window.lua. Avoids the 2x boundary-burst problem of a naive
// fixed window without storing a timestamp per request.
package limiter

import(
	"context"
	"time"
	"fmt"
	"github.com/redis/go-redis/v9"
	_ "embed"
)

//go:embed lua/sliding_window.lua
var slidingWindowScriptSrc string

type SlidingWindowConfig struct{
	Limit int
	WindowSize time.Duration
	KeyPrefix string
	TTL time.Duration
}

type SlidingWindow struct{
	rdb redis.Cmdable
	script *redis.Script
	cfg SlidingWindowConfig
}

func NewSlidingWindow(rdb redis.Cmdable, cfg SlidingWindowConfig) *SlidingWindow {
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "sw"
	}
	if cfg.TTL <= 0 {
		cfg.TTL = cfg.WindowSize*2 + 60*time.Second
	}
	return &SlidingWindow{
		rdb:    rdb,
		script: redis.NewScript(slidingWindowScriptSrc),
		cfg:    cfg,
	}
}

func (sw *SlidingWindow) Allow(ctx context.Context, clientID string) (Decision, error) {
	windowMs := sw.cfg.WindowSize.Milliseconds()
	nowMs := time.Now().UnixMilli()

	// Which fixed window does "now" fall into, and which is the one
	// immediately before it. Integer division gives us a stable,
	// monotonically increasing window index that every gateway instance
	// computes identically from the same wall clock -- no coordination
	// needed beyond clocks being roughly in sync.
	currIdx := nowMs / windowMs
	prevIdx := currIdx - 1

	currKey := fmt.Sprintf("%s:{%s}:%d", sw.cfg.KeyPrefix, clientID, currIdx)
	prevKey := fmt.Sprintf("%s:{%s}:%d", sw.cfg.KeyPrefix, clientID, prevIdx)
	fmt.Printf("DEBUG: limit=%v window_seconds=%v now_ms=%v requested=%v ttl_seconds=%v\n",
    sw.cfg.Limit, int64(sw.cfg.WindowSize.Seconds()), nowMs, 1, int(sw.cfg.TTL.Seconds()))
	// res, err := sw.script.Run(ctx, sw.rdb, []string{currKey, prevKey},
	// 	sw.cfg.Limit,
	// 	int64(sw.cfg.WindowSize.Seconds()),
	// 	nowMs,
	// 	1, // requested cost
	// 	int(sw.cfg.TTL.Seconds()),
	// ).Result()
	res, err := sw.script.Run(ctx, sw.rdb, []string{currKey, prevKey},
		sw.cfg.Limit,
		int64(sw.cfg.WindowSize.Seconds()),
		nowMs,
		1,
		int(sw.cfg.TTL.Seconds()),
	).Result()

	fmt.Printf("DEBUG: currKey=%q prevKey=%q res=%#v err=%v errIsNil=%v\n",
		currKey, prevKey, res, err, err == redis.Nil)

	if err != nil {
		return Decision{}, fmt.Errorf("sliding window allow: %w", err)
	}
	if err != nil {
		return Decision{}, fmt.Errorf("sliding window allow: %w", err)
	}

	allowed, remaining, retryAfterMs, err := parseDecisionReply(res)
	if err != nil {
		return Decision{}, fmt.Errorf("sliding window allow: %w", err)
	}

	return Decision{
		Allowed:    allowed,
		Remaining:  remaining,
		RetryAfter: time.Duration(retryAfterMs) * time.Millisecond,
	}, nil
}

func parseDecisionReply(res interface{}) (allowed bool, remaining int, retryAfterMs int64, err error) {
	arr, ok := res.([]interface{})
	if !ok || len(arr) != 3 {
		return false, 0, 0, fmt.Errorf("unexpected script reply shape: %#v", res)
	}

	allowedInt, ok := arr[0].(int64)
	if !ok {
		return false, 0, 0, fmt.Errorf("unexpected 'allowed' type: %#v", arr[0])
	}
	remainingInt, ok := arr[1].(int64)
	if !ok {
		return false, 0, 0, fmt.Errorf("unexpected 'remaining' type: %#v", arr[1])
	}
	retryMs, ok := arr[2].(int64)
	if !ok {
		return false, 0, 0, fmt.Errorf("unexpected 'retry_after_ms' type: %#v", arr[2])
	}

	return allowedInt == 1, int(remainingInt), retryMs, nil
}