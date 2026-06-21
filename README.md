# Distributed Rate Limiter & Mini API Gateway

A rate limiter that has to stay correct when multiple service instances share
the same client traffic — the failure mode every naive in-memory limiter hits
the moment you run more than one process behind a load balancer. Full design
rationale lives in [`RATELIMITER_PROBLEM_STATEMENT.md`](./RATELIMITER_PROBLEM_STATEMENT.md).

The core idea: state lives in Redis, not in any one process, and every
check-then-increment is done atomically inside a Lua script so concurrent
requests from the same client can never both slip through a stale read.

## Algorithms

Both implemented against the same Redis-backed atomic core so they can be
benchmarked head to head instead of compared on paper.

| Algorithm | How it works | Tradeoff |
|---|---|---|
| Token bucket | Bucket refills at a steady rate; each request costs a token | Allows controlled bursts up to bucket size, cheap to compute |
| Sliding window counter | Weighted average of current + previous fixed window | Avoids the 2x boundary-burst problem without storing a timestamp per request |

## Architecture

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
                    │ Toy backend service │
                    │ (protected resource)│
                    └────────────────────┘
```

A denied request gets a `429` with a `Retry-After` computed from the
algorithm's own state — not a hardcoded number — so a client that waits
exactly that long is guaranteed to get back in.

## API

```
ANY  /api/*           protected route, passes through the rate limiter
                       → 200 + response, headers:
                           X-RateLimit-Limit
                           X-RateLimit-Remaining
                       → 429 if denied, header:
                           Retry-After: <seconds>

GET  /admin/stats      per-client allowed/denied counts (debug)
```

## Project layout

```
cmd/
  loadtest/      concurrent load-test client + correctness report — done
  gateway/       entry point wiring auth → limiter → logging → backend — not yet written
internal/
  limiter/       Limiter interface + token bucket / sliding window, each
                 backed by an atomic Lua script                          — done
    lua/         the actual atomicity guarantee (runs inside Redis)      — done
  middleware/    auth (API key extraction), rate-limit (429 + headers),
                 logging (per-client allowed/denied counters)            — done
  backend/       toy echo service the gateway forwards allowed requests to — done
  config/        env-driven config (algorithm, port, Redis addr, limits) — done
  stats/         backs GET /admin/stats                                  — stub, not wired up
deploy/          Dockerfile + docker-compose (redis + 4 gateway instances) — written, but build
                 currently fails: it targets ./cmd/gateway, which doesn't exist yet
scripts/         local run + benchmark convenience scripts               — placeholders only
results/         loadtest output (json/csv) for the README's numbers     — not created yet
```

## Status

**Building.** The pieces that prove the hard part — atomic Lua scripts,
the HTTP middleware chain, the concurrent load-test harness — are written.
What's missing is the glue that turns them into a runnable demo:

- [x] Token bucket limiter, Redis-backed, atomic via Lua
- [x] Sliding window counter limiter, Redis-backed, atomic via Lua
- [x] Auth / rate-limit / logging middleware
- [x] Toy backend + env-driven config
- [x] Concurrent load-test client with per-target and per-second reporting
- [ ] `cmd/gateway` entry point that actually wires the above into a server
- [ ] `internal/stats` reading from the logging middleware's counters
- [ ] `scripts/run_local.sh` and `scripts/run_benchmarks.sh` implementations
- [ ] A load test run against real multi-instance Redis-backed gateways, with
      results checked into `results/` and the summary table pasted below

## Running it today

Until `cmd/gateway` exists, the Docker Compose / end-to-end path won't build.
What you can run right now:

```bash
go vet ./...
go build ./internal/...   # limiter, middleware, backend, config all compile standalone
```

Once the gateway entry point lands, the intended flow is:

```bash
docker compose -f deploy/docker-compose.yml up --build   # redis + 4 gateway instances
go run ./cmd/loadtest --algo=token_bucket   --targets=:8083,:8084
go run ./cmd/loadtest --algo=sliding_window --targets=:8081,:8082
```

## Tech stack

- **Go** — goroutines for the concurrent load-test client, `net/http` for the
  gateway, [`go-redis`](https://github.com/redis/go-redis) for the client and
  Lua script execution.
- **Redis** — the shared state that makes "distributed" real rather than
  simulated.
- **Docker Compose** — one command to bring up Redis + multiple gateway
  instances once the gateway exists.

## Out of scope (v1)

- Multi-region / multi-Redis-cluster coordination
- Per-route or per-tier (free vs. paid) differentiated limits
- Any real business logic behind the gateway — the toy backend exists only
  to give the gateway something to protect
