# nexus

A production-grade API gateway written in Go. Nexus sits in front of external APIs and provides authentication, rate limiting, caching, circuit breaking, and observability out of the box.

Built as a portfolio project demonstrating backend infrastructure patterns. Fork it, swap in your own upstream APIs, and run it.

## Features

- **JWT + API key authentication** — register/login flow with refresh token rotation and a Redis-backed denylist for instant revocation
- **Tiered rate limiting** — sliding window per user (100 req/min for JWT, 500 for API keys), Redis-backed with `Retry-After` headers
- **Response caching** — Redis cache with singleflight deduplication (concurrent identical requests hit the upstream only once)
- **Circuit breaker** — per-upstream breaker opens after 5 consecutive failures, preventing cascade failures
- **REST + GraphQL** — both interfaces expose the same data; use whichever fits your client
- **Prometheus metrics** — request counts, latencies, and cache hit/miss rates at `/metrics`
- **Distributed tracing** — OpenTelemetry traces sent to Jaeger

### Upstream APIs included

| API | What it provides |
|-----|-----------------|
| GitHub REST API | User profiles, repository metadata |
| IBKR Client Portal | Live/delayed market quotes, OHLCV history, contract search |

---

## Architecture

```
                          ┌─────────────────────────────────┐
                          │             nexus               │
                          │                                 │
Client ──► /auth/*  ──────┤  Auth handlers (no middleware)  │
                          │                                 │
Client ──► /api/v1/* ─────┤  JWT / API key middleware       │
           /graphql       │         │                       │
                          │  Rate limit middleware          │
                          │         │                       │
                          │  Cache middleware               │
                          │    (singleflight + Redis)       │
                          │         │                       │
                          │  Circuit breaker                │
                          │         │                       │
                          └─────────┼───────────────────────┘
                                    │
                    ┌───────────────┼───────────────┐
                    ▼               ▼               ▼
              GitHub API       IBKR Client      Postgres
                              Portal Gateway    (user data)
```

### Package layout

```
cmd/server/          — main entry point
config/              — env-based config loading
internal/
  auth/              — JWT, API keys, refresh tokens, denylist, middleware
  cache/             — Redis middleware with singleflight
  circuit/           — per-upstream circuit breaker (sony/gobreaker)
  db/                — Postgres pool + models
    queries/         — raw SQL queries (users, api_keys, saved_searches, refresh_tokens)
    migrations/      — SQL migration files
  gateway/
    rest/            — GitHub and IBKR REST handlers
  graph/             — GraphQL schema, resolvers, DataLoader
  ibkr/              — IBKR Client Portal API client
  metrics/           — Prometheus instrumentation
  ratelimit/         — sliding window rate limiter + chi middleware
  tracing/           — OpenTelemetry setup
  upstream/          — generic HTTP client shared by REST handlers
```

---

## Quickstart

### Option A — Docker (recommended)

Requires Docker Desktop.

```bash
git clone https://github.com/meteaksoyy/nexus
cd nexus

cp .env.example .env
# Edit .env — set JWT_SECRET to a random string (everything else has defaults)

docker-compose up --build
```

This starts Postgres, Redis, Jaeger, and the nexus server together. The server is ready at `http://localhost:8080`.

### Option B — Local Go

Requires Go 1.23+, Postgres, and Redis.

```bash
# Start infrastructure only
docker-compose up postgres redis jaeger -d

cp .env.example .env
# Edit .env

# Install goose for migrations
go install github.com/pressly/goose/v3/cmd/goose@latest

# Run migrations
goose -dir internal/db/migrations postgres "postgres://nexus:nexus@localhost:5432/nexus" up

# Run
go run ./cmd/server
```

---

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP listen port |
| `ENV` | `development` | Set to `production` for JSON logs |
| `JWT_SECRET` | — | **Required.** Long random string for signing JWTs |
| `JWT_EXPIRY_MINUTES` | `60` | JWT lifetime |
| `JWT_REFRESH_DAYS` | `7` | Refresh token lifetime |
| `DATABASE_URL` | — | **Required.** Postgres connection string |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection string |
| `GITHUB_TOKEN` | — | GitHub personal access token (optional, raises API rate limit) |
| `IBKR_GATEWAY_URL` | `https://localhost:5000` | IBKR Client Portal Gateway URL |
| `IBKR_USERNAME` | — | IBKR username for auto-auth (optional) |
| `IBKR_PASSWORD` | — | IBKR password for auto-auth (optional) |
| `RATE_LIMIT_AUTHED` | `100` | Requests per window for JWT clients |
| `RATE_LIMIT_APIKEY` | `500` | Requests per window for API key clients |
| `RATE_LIMIT_WINDOW_SECONDS` | `60` | Rate limit window size |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | Jaeger OTLP endpoint |

---

## API reference

### Authentication

All endpoints under `/api/v1/` and `/graphql` require either:
- `Authorization: Bearer <jwt>` header
- `X-API-Key: <key>` header

```bash
# Register
POST /auth/register
{"email": "you@example.com", "password": "secret"}

# Login → JWT + refresh token
POST /auth/token
{"email": "you@example.com", "password": "secret"}

# Refresh JWT
POST /auth/refresh
{"refresh_token": "<token>"}

# Logout (revokes JWT + refresh token)
POST /auth/logout
{"refresh_token": "<token>"}

# Create API key
POST /auth/apikey          (requires JWT)
{"name": "my-key"}

# Delete API key
DELETE /auth/apikey/{id}   (requires JWT)
```

### REST endpoints

```bash
# GitHub
GET /api/v1/github/users/{login}
GET /api/v1/github/repos/{owner}/{repo}

# IBKR market data (requires IBKR Client Portal Gateway running)
GET /api/v1/market/quote?symbol=AAPL
GET /api/v1/market/history?symbol=AAPL&period=1d&bar=1h
GET /api/v1/market/search?symbol=AAPL
```

Cache headers are set automatically. Responses are cached in Redis (5 min for GitHub, 30s for quotes, 5 min for history, 1h for contract search).

### GraphQL — `POST /graphql`

```graphql
# Current user
query {
  me {
    id
    email
    role
    savedSearches {
      id
      query
      createdAt
    }
  }
}

# GitHub user profile
query {
  githubUser(login: "torvalds") {
    name
    bio
    publicRepos
    followers
  }
}

# GitHub repository
query {
  githubRepo(owner: "golang", name: "go") {
    stars
    forks
    language
    description
  }
}

# Market quote (requires IBKR)
query {
  quote(symbol: "AAPL") {
    last
    bid
    ask
    change
    changePct
    volume
    currency
  }
}

# OHLCV history (requires IBKR)
query {
  marketHistory(symbol: "AAPL", period: "1d", bar: "1h") {
    symbol
    bars {
      time
      open
      high
      low
      close
      volume
    }
  }
}

# Save a search (mutation)
mutation {
  saveSearch(query: "AAPL earnings") {
    id
    query
    createdAt
  }
}
```

### Observability

```
GET /health    → {"status":"ok"}
GET /metrics   → Prometheus metrics
```

Jaeger UI (traces): `http://localhost:16686`

---

## IBKR setup

The IBKR endpoints require the **Client Portal Gateway** — a Java app from Interactive Brokers that runs locally and proxies requests to their API.

1. Download from [Interactive Brokers API](https://www.interactivebrokers.com/en/trading/ib-api.html) → Client Portal Gateway
2. Start it: `java -jar root/run.jar root/conf.yaml`
3. Either authenticate manually at `https://localhost:5000` in your browser, or set `IBKR_USERNAME` and `IBKR_PASSWORD` in `.env` for auto-auth
4. Start nexus — it will keep the session alive automatically

> **Note:** You need an Interactive Brokers account. A paper trading account works fine. If you don't have one, all other endpoints still work normally.

---

## Running tests

```bash
# Unit tests (no external dependencies)
go test ./internal/... -race

# Integration tests (requires Docker for Postgres + Redis containers)
go test -tags integration -v ./tests/integration/... -timeout 120s

# Load test (requires k6 and a running server)
k6 run tests/load/github_user.js -e BASE_URL=http://localhost:8080 -e JWT=<token>
```

---

## Design decisions

**Why a sliding window rate limiter instead of token bucket?**
Sliding window log gives exact request counts with no burst allowance — better for a gateway where fairness matters. The tradeoff is O(n) Redis memory per client, acceptable at these scales.

**Why singleflight for cache misses?**
Without it, a cache miss for a popular key causes a thundering herd — all concurrent requests hit the upstream simultaneously. Singleflight ensures only one upstream call is made, with all waiters receiving the same result.

**Why `InsecureSkipVerify` for IBKR?**
The IBKR Client Portal Gateway serves a self-signed certificate on `localhost:5000`. TLS verification is skipped only for the IBKR client; all other upstream clients use standard TLS.

**Why DataLoader for GraphQL?**
The `me.savedSearches` field would otherwise cause an N+1 query — one DB call per user in a batch. The DataLoader batches all user IDs within a single request tick into one `WHERE user_id = ANY($1)` query.

**Why not sqlc / an ORM?**
Raw SQL in `internal/db/queries/` keeps the queries explicit and easy to audit. The Makefile has a `gen` target for sqlc if you want to adopt it.
