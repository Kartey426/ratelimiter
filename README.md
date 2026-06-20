# Distributed Rate Limiter & Mini API Gateway

See `RATELIMITER_PROBLEM_STATEMENT.md` for the full design rationale.

## Layout

```
cmd/
  gateway/       entry point for one gateway instance (run 2+ for the demo)
  loadtest/      THE deliverable: concurrent load-test client + correctness report
internal/
  limiter/       algorithm-agnostic Limiter interface + token bucket / sliding
                 window implementations, each backed by an atomic Lua script
    lua/         the actual atomicity guarantee (runs inside Redis, no race window)
  middleware/    auth (client ID extraction), rate-limit (429 + headers), logging
  backend/       toy protected resource the gateway forwards allowed requests to
  stats/         backs GET /admin/stats
  config/        env-driven config shared by gateway + loadtest
deploy/          Dockerfile + docker-compose (redis + 2 gateway instances)
scripts/         local run + benchmark convenience scripts
results/         loadtest output (json/csv) + the numbers that go in this README
```

## Run order

1. `docker compose -f deploy/docker-compose.yml up` — redis + 2 gateways
2. `go run ./cmd/loadtest --algo=token_bucket --targets=:8081,:8082 ...`
3. `go run ./cmd/loadtest --algo=sliding_window --targets=:8081,:8082 ...`
4. Compare `results/*.json`, paste the summary table + chart here.

## Status
Building
