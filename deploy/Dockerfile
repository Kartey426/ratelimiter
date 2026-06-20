# deploy/Dockerfile
#
# Multi-stage build for cmd/gateway. Build context must be the repo root
# (not deploy/) since it needs go.mod, go.sum, and all internal packages.
#   docker build -f deploy/Dockerfile -t ratelimiter-gateway .
# docker-compose.yml below sets this automatically (context: ..).

FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN wc -c internal/limiter/lua/sliding_window.lua
RUN CGO_ENABLED=0 go build -o /out/gateway ./cmd/gateway

FROM alpine:3.21
COPY --from=build /out/gateway /usr/local/bin/gateway
ENTRYPOINT ["/usr/local/bin/gateway"]
