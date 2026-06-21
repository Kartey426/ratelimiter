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

| Algorithm              | How it works                                                | Tradeoff                                                                     |
| ---------------------- | ----------------------------------------------------------- | ---------------------------------------------------------------------------- |
| Token bucket           | Bucket refills at a steady rate; each request costs a token | Allows controlled bursts up to bucket size, cheap to compute                 |
| Sliding window counter | Weighted average of current + previous fixed window         | Avoids the 2x boundary-burst problem without storing a timestamp per request |

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

## Results

Early runs against the dockerized gateways showed `total_unreachable` counts
ranging from the low hundreds up to ~33K — a Windows→Docker port-forwarding
issue on the test machine, since fixed. The table below includes only runs
with zero unreachable requests.

| Algorithm      | Sent    | Allowed | Denied  | Instances     | Allowed vs. predicted                                               |
| -------------- | ------- | ------- | ------- | ------------- | ------------------------------------------------------------------- |
| Sliding window | 7,401   | 50      | 7,351   | :8081 / :8082 | 5 clients × 10/window = **50**, exact                               |
| Sliding window | 27,290  | 200     | 27,090  | :8081 / :8082 | 20 clients × 10/window = **200**, exact (98/102 split)              |
| Sliding window | 242,149 | 120     | 242,029 | :8081 / :8082 | 12 clients × 10/window = **120**, exact (56/64 split)               |
| Token bucket   | 13,317  | 100     | 13,217  | :8083 / :8084 | 5 clients × (10 burst + 10 refilled) = **100**, exact (58/42 split) |

In every clean run, the allowed count lands exactly on the analytically
predicted limit regardless of scale (7K to 242K requests sent) or how the
load happened to split across the two independent gateway processes — which
is the actual evidence that one global quota is enforced through Redis,
rather than each instance keeping (and being fooled by) its own counter.

## Project layout

```
cmd/
  loadtest/      concurrent load-test client + correctness report — done
  gateway/       entry point wiring auth → limiter → logging → backend — done
internal/
  limiter/       Limiter interface + token bucket / sliding window, each
                 backed by an atomic Lua script                          — done
    lua/         the actual atomicity guarantee (runs inside Redis)      — done
  middleware/    auth (API key extraction), rate-limit (429 + headers),
                 logging (per-client allowed/denied counters)            — done
  backend/       toy echo service the gateway forwards allowed requests to — done
  config/        env-driven config (algorithm, port, Redis addr, limits) — done
  stats/         backs GET /admin/stats                                  — stub, not wired up
deploy/          Dockerfile + docker-compose (redis + 4 gateway instances) — done
scripts/         local run + benchmark convenience scripts               — placeholders only
results/         loadtest output (json/csv) backing the Results section above — summarized
                 here, but the raw files aren't committed yet
```

## Status

**Working end-to-end.** Both algorithms, the full middleware chain, and the
gateway entry point are done and proven correct under concurrent load (see
Results above). Two things are still loose ends: `internal/stats` (the
`GET /admin/stats` endpoint isn't wired up yet) and `scripts/` (the
convenience scripts are placeholders — running things below is done by hand
for now).

## Running it

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
- **Docker Compose** — one command to bring up Redis and multiple gateway
  instances.

## Out of scope (v1)

- Multi-region / multi-Redis-cluster coordination
- Per-route or per-tier (free vs. paid) differentiated limits
- Any real business logic behind the gateway — the toy backend exists only
  to give the gateway something to protect
