-- token_bucket.lua
--
-- Atomic check-and-consume for the token bucket algorithm. Redis runs the
-- whole script as a single operation, so two concurrent callers can never
-- both read the same "tokens remaining" value before either one writes
-- back -- this is the fix for the race condition described in the problem
-- statement.
--
-- KEYS[1] = bucket hash key for this client, e.g. "tb:{client_id}"
--
-- ARGV[1] = capacity        (max tokens the bucket can hold, integer/float)
-- ARGV[2] = refill_rate     (tokens added per second, float)
-- ARGV[3] = now_ms          (current time in unix milliseconds, integer)
-- ARGV[4] = requested       (tokens this request costs, usually 1)
-- ARGV[5] = ttl_seconds     (key expiry so idle clients don't leak memory)
--
-- Returns an array: { allowed, remaining, retry_after_ms }
--   allowed         1 or 0
--   remaining       tokens left in the bucket AFTER this call, floored to int
--   retry_after_ms  0 if allowed; otherwise the time until enough tokens
--                   will have accrued to satisfy ARGV[4]
--
-- Storage: a Redis hash with fields:
--   tokens          current token count (float, stored as string)
--   last_refill_ms  unix ms timestamp of the last refill computation
--
-- Lazy refill model: we don't run a background ticker. Instead each call
-- computes "how much time passed since we last looked at this bucket" and
-- credits tokens for exactly that elapsed time. This keeps the bucket
-- correct regardless of how bursty or sparse traffic is, and means an
-- idle bucket naturally catches back up to full on the next request.

local key            = KEYS[1]

local capacity       = tonumber(ARGV[1])
local refill_rate    = tonumber(ARGV[2])
local now_ms         = tonumber(ARGV[3])
local requested      = tonumber(ARGV[4])
local ttl_seconds    = tonumber(ARGV[5])

if capacity <= 0 or refill_rate <= 0 or requested <= 0 then
    return redis.error_reply("token_bucket: capacity, refill_rate, and requested must be > 0")
end

-- Load existing state, or initialize a fresh full bucket if this is the
-- client's first request (HMGET returns false for missing fields).
local state = redis.call("HMGET", key, "tokens", "last_refill_ms")
local tokens          = tonumber(state[1])
local last_refill_ms  = tonumber(state[2])

if tokens == nil or last_refill_ms == nil then
    tokens = capacity
    last_refill_ms = now_ms
end

-- Refill based on elapsed time. Clamp elapsed to >= 0 in case of clock
-- skew across instances (e.g. a request timestamped slightly behind the
-- last write) so we never go negative or double-credit.
local elapsed_ms = now_ms - last_refill_ms
if elapsed_ms < 0 then
    elapsed_ms = 0
end

local elapsed_seconds = elapsed_ms / 1000.0
local refilled_tokens = tokens + (elapsed_seconds * refill_rate)
if refilled_tokens > capacity then
    refilled_tokens = capacity
end

local allowed = 0
local retry_after_ms = 0

if refilled_tokens >= requested then
    -- Enough tokens: consume and allow.
    allowed = 1
    refilled_tokens = refilled_tokens - requested
else
    -- Not enough: compute exactly how long until there will be. This is
    -- what makes Retry-After honest -- a client that waits this long and
    -- retries is guaranteed to have enough tokens (modulo other clients
    -- draining the bucket further in the meantime, which is correct
    -- behavior under shared/contended load).
    local deficit = requested - refilled_tokens
    local seconds_needed = deficit / refill_rate
    retry_after_ms = math.ceil(seconds_needed * 1000)
end

-- Persist state regardless of allow/deny: last_refill_ms must always
-- advance to "now" so we don't re-credit the same elapsed window on the
-- next call, and tokens must reflect the post-refill (pre- or
-- post-consume) value.
redis.call("HMSET", key, "tokens", tostring(refilled_tokens), "last_refill_ms", tostring(now_ms))
redis.call("EXPIRE", key, ttl_seconds)

local remaining = math.floor(refilled_tokens)
if remaining < 0 then
    remaining = 0
end

return { allowed, remaining, retry_after_ms }
