# Distributed Rate Limiter & Mini API Gateway — Problem Statement & Design

## The Problem

Every public API needs to protect itself from a client sending too many requests too
fast — whether that's abuse, a buggy retry loop, or just a noisy neighbor starving
other clients of capacity. This is why APIs like Stripe, GitHub, and Twitter return
`429 Too Many Requests` once you cross a threshold, usually with a `Retry-After`
header telling you when you're allowed back in.

The naive version of this is easy: keep a counter per client in memory, reset it
every minute, reject requests once the counter is exceeded. It's also wrong the
moment your service runs more than one instance behind a load balancer — each
instance has its own counter, so a client can get `N` requests through *per
instance*, not `N` total. The limiter only works if all instances agree on how much
quota a client has used, which means the counting state has to live somewhere
shared, not in any one process's memory.

That's the actual problem this project solves: **correct rate limiting under
concurrent access, across multiple service instances, with no single instance ever
seeing the full picture on its own.**

## Why This Is Harder Than It Looks

The bug almost everyone writes by accident is a check-then-act race condition:

```
1. read current_count for client X        →  count = 9 (limit is 10)
2. if count < limit: allow request
3. increment count                          →  count = 10
```

Two requests from the same client, handled concurrently by two goroutines (or two
service instances), can both read `count = 9` before either one writes back. Both
get allowed through. The client just got 11 requests on a limit of 10, and it gets
worse the more concurrent traffic you throw at it. This only shows up under real
concurrent load — it will look correct in any single-threaded manual test, which is
exactly why it ships broken so often.

The fix is to make "check and increment" a single atomic operation, not two steps.
Redis gives you this via a Lua script (Redis guarantees a script runs atomically,
with no other command interleaving) or via `INCR` + a careful TTL setup. The whole
design exercise is: pick an algorithm, then make every decision atomic against
shared state.

## Algorithms (and why you're implementing more than one)

| Algorithm | How it works | Tradeoff |
|---|---|---|
| Fixed window | Count resets every N seconds (e.g. on the minute) | Simple, but allows 2x burst at window boundaries (client sends max requests at 0:59 and again at 1:00) |
| Token bucket | Bucket refills at a steady rate, each request costs a token | Allows controlled bursts up to bucket size, smooths out over time, cheap to compute |
| Sliding window counter | Weighted average of current + previous window | Avoids the boundary burst problem without the memory cost of storing every timestamp |

Implement at least token bucket and sliding window counter, both against the same
Redis-backed atomic core, and benchmark them side by side. The point isn't "which
one is best" — it's that you can explain the tradeoff with your own numbers instead
of reciting it from a system-design blog post.

## Architecture: framed as a mini API gateway, not a bare utility

A rate limiter with nothing behind it is just a function call. To make it a real
system, wrap it the way an actual gateway would:

```
Client
  │
  ▼
┌─────────────────────────────────────────┐
│  Gateway (Go, net/http)                  │
│  ┌─────────────┐  ┌──────────────────┐  │
│  │ Auth/API key│→ │ Rate Limiter      │  │
│  │ middleware  │  │ (token bucket /   │  │
│  │             │  │  sliding window,  │  │
│  │             │  │  Redis-backed)    │  │
│  └─────────────┘  └────────┬─────────┘  │
│                             │ allowed     │
│                    ┌────────▼─────────┐  │
│                    │ Request logging  │  │
│                    └────────┬─────────┘  │
└─────────────────────────────┼────────────┘
                               ▼
                    ┌────────────────────┐
                    │ Toy backend service │  (anything — even an
                    │ (protected resource)│   echo endpoint is fine)
                    └────────────────────┘
```

If the limiter denies the request, the gateway returns `429` with
`Retry-After` computed from the algorithm's own state (when the bucket next
has a token / when the window rolls over) — and that header has to be *honest*:
a client that waits exactly that long and retries should get back in.

## Tech Stack

- **Go** — goroutines for the concurrent load-test client, `net/http` for the
  gateway itself, `go-redis` for the Redis client and Lua script execution.
- **Redis** — shared state across instances; this is the one external dependency
  that makes the "distributed" part real rather than simulated.
- **Docker Compose** — gateway + Redis, one command to run the whole thing.

## API / Behavior

```
ANY  /api/*                 protected route, passes through rate limiter
                             → 200 + response, with headers:
                                 X-RateLimit-Limit
                                 X-RateLimit-Remaining
                             → 429 if denied, with header:
                                 Retry-After: <seconds>

GET  /admin/stats           current usage per client (for demo/debugging)
```

## Proving It Works: the actual deliverable

The schema and the algorithm are not the deliverable — the demonstration that it
holds under concurrency is. Build a load-test client (a Go program using
goroutines, not a shell script with curl in a loop) that:

1. Spins up N concurrent "clients," each firing requests as fast as possible.
2. Runs against **multiple gateway instances** simultaneously (e.g. two processes
   on different ports, same Redis) to prove the limit is enforced globally, not
   per-instance.
3. Reports: total requests sent, total allowed, total denied, and whether allowed
   count ever exceeded the configured limit (it shouldn't, ever, under any
   concurrency level — this is the actual correctness claim).
4. Repeats the test for each algorithm and reports the burst/smoothing difference
   between token bucket and sliding window with real numbers.

That output — "ran 10,000 concurrent requests across 2 instances at a configured
limit of 100/sec, allowed exactly 100/sec, zero overage, here's the chart" — is the
whole point of the project and the actual resume bullet.

## Explicitly Out of Scope (v1)

- Multi-region / multi-Redis-cluster coordination.
- Per-route or per-tier (free vs paid) differentiated limits — fine as a stretch
  goal once the core is proven correct, not before.
- Anything resembling real business logic behind the gateway. The toy backend
  exists only to give the gateway something to protect; it is intentionally
  uninteresting.

## What This Demonstrates

Atomic operations under concurrency, distributed (not per-process) state
management, Go's concurrency model used for both the system and its own test
harness, and — same habit as your other projects — a quantified, load-tested proof
that the thing actually does what it claims under pressure rather than just "looks
right" in a single manual request.
