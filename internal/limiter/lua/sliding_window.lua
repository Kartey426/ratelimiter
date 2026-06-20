-- sliding_window.lua
--
-- Atomic check-and-increment for the sliding window counter algorithm.
-- Approximates a true sliding window by taking a weighted average of the
-- previous fixed window's count and the current fixed window's count,
-- where the weight is how much of the previous window still "overlaps"
-- the lookback period. This avoids the 2x boundary-burst problem of a
-- naive fixed window (max requests at 0:59 AND again at 1:00) without
-- paying the memory cost of storing a timestamp per request (which a
-- true sliding log would require).
--
-- KEYS[1] = current window counter key,  e.g. "sw:{client_id}:{curr_idx}"
-- KEYS[2] = previous window counter key, e.g. "sw:{client_id}:{curr_idx-1}"
--
-- ARGV[1] = limit            (max requests allowed per window)
-- ARGV[2] = window_seconds   (fixed window size, e.g. 60)
-- ARGV[3] = now_ms           (current time in unix milliseconds)
-- ARGV[4] = requested        (cost of this request, usually 1)
-- ARGV[5] = ttl_seconds      (expiry for both window keys)
--
-- Returns an array: { allowed, remaining, retry_after_ms }
--   allowed         1 or 0
--   remaining       limit - estimated_count AFTER this call, floored,
--                   clamped to >= 0
--   retry_after_ms  0 if allowed; otherwise time until the weighted
--                   estimate will have decayed enough to admit this
--                   request, assuming no further traffic
--
-- The caller (Go side) is responsible for computing which window index
-- "now" falls into and passing the correct current/previous keys -- this
-- script only does the atomic math and the increment, it doesn't decide
-- which keys to use.

local curr_key       = KEYS[1]
local prev_key       = KEYS[2]

local limit          = tonumber(ARGV[1])
local window_seconds  = tonumber(ARGV[2])
local now_ms          = tonumber(ARGV[3])
local requested        = tonumber(ARGV[4])
local ttl_seconds      = tonumber(ARGV[5])

if limit <= 0 or window_seconds <= 0 or requested <= 0 then
    return redis.error_reply("sliding_window: limit, window_seconds, and requested must be > 0")
end

local window_ms = window_seconds * 1000

-- Position of "now" within the current window, in [0, window_ms).
-- elapsed_in_window = 0 means we just crossed a window boundary (previous
-- window's full count should still weigh heavily); elapsed_in_window close
-- to window_ms means we're about to roll over (previous window barely
-- matters anymore).
local elapsed_in_window = now_ms % window_ms

-- Weight = fraction of the previous window that's still "in view" of a
-- trailing window_seconds-wide lookback ending at now. This is the
-- standard sliding-window-counter formula.
local weight = (window_ms - elapsed_in_window) / window_ms
if weight < 0 then weight = 0 end
if weight > 1 then weight = 1 end

local curr_count = tonumber(redis.call("GET", curr_key) or "0")
local prev_count = tonumber(redis.call("GET", prev_key) or "0")

local estimated_count = curr_count + (prev_count * weight)

local allowed = 0
local retry_after_ms = 0

if estimated_count + requested <= limit then
    allowed = 1
    curr_count = redis.call("INCRBY", curr_key, requested)
    redis.call("EXPIRE", curr_key, ttl_seconds)
    -- Recompute the post-increment estimate for an accurate `remaining`.
    estimated_count = curr_count + (prev_count * weight)
else
    -- Not allowed: estimate how long until the weighted count decays
    -- below the limit, assuming no further requests arrive. The previous
    -- window's contribution decays linearly as `weight` shrinks toward 0
    -- over the rest of the current window; once `weight` hits 0 the
    -- current window's own count is all that's left, which is already
    -- known (and already <= limit, or we wouldn't be denying based on a
    -- decaying prev_count alone). We solve for how much `weight` must
    -- drop for estimated_count to fit.
    local overflow = (estimated_count + requested) - limit
    if prev_count > 0 then
        -- weight needs to drop by (overflow / prev_count); weight drops
        -- linearly at a rate of 1 / window_ms per ms.
        local weight_drop_needed = overflow / prev_count
        local ms_needed = weight_drop_needed * window_ms
        if ms_needed < 0 then ms_needed = 0 end
        retry_after_ms = math.ceil(ms_needed)
    else
        -- No previous-window contribution; the current window itself is
        -- saturated, so the client has to wait for the window to roll
        -- over entirely.
        retry_after_ms = math.ceil(window_ms - elapsed_in_window)
    end
end

local remaining = math.floor(limit - estimated_count)
if remaining < 0 then
    remaining = 0
end

return { allowed, remaining, retry_after_ms }
