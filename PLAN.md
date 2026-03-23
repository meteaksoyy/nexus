# nexus — API Gateway

> A production-grade API gateway written in Go that unifies REST and GraphQL access to multiple upstream services, with built-in auth, rate limiting, and Redis caching.

---

## Project Goals

Build a gateway that:
- Exposes a **single GraphQL endpoint** that federates data from multiple upstream REST APIs and a local Postgres database
- Enforces **JWT and API key authentication** on all inbound requests
- Applies **per-client rate limiting** using a sliding window algorithm backed by Redis
- **Caches** upstream responses in Redis with TTL-based invalidation
- Is fully containerised and observable (structured logs, Prometheus metrics)

This is a portfolio project targeting backend/infrastructure engineering roles.

---

## Name

**`nexus`** — main directory name: `nexus/`

---

## Architecture Overview

```
Client
  │
  ▼
┌─────────────────────────────────┐
│           nexus gateway         │
│                                 │
│  ┌──────────┐  ┌─────────────┐  │
│  │ REST API │  │  GraphQL    │  │
│  │ /api/v1  │  │  /graphql   │  │
│  └────┬─────┘  └──────┬──────┘  │
│       │               │         │
│  ┌────▼───────────────▼──────┐  │
│  │      Middleware Chain      │  │
│  │  Auth → RateLimit → Cache  │  │
│  └────────────┬──────────────┘  │
│               │                 │
│  ┌────────────▼──────────────┐  │
│  │       Resolver Layer       │  │
│  └──┬──────────┬─────────────┘  │
│     │          │                │
└─────┼──────────┼────────────────┘
      │          │
      ▼          ▼
  Upstream    Postgres
  REST APIs     DB
  (GitHub,
   Weather)
```

---

## Upstream Services

| Service | What we use it for | Auth |
|---|---|---|
| GitHub REST API | Repo metadata, user info | GitHub PAT (env var) |
| Open-Meteo API | Weather data by coordinates | None (public) |
| Postgres (local) | User profiles, saved searches | Internal |

These three give enough variety to make the GraphQL schema interesting without requiring paid API keys.

---

## Directory Structure

```
nexus/
├── cmd/
│   └── server/
│       └── main.go               # Entrypoint
├── internal/
│   ├── auth/
│   │   ├── jwt.go                # JWT validation + claims extraction
│   │   ├── apikey.go             # API key lookup against DB
│   │   └── middleware.go         # Auth middleware (checks both)
│   ├── ratelimit/
│   │   ├── sliding_window.go     # Sliding window counter in Redis
│   │   └── middleware.go         # Per-client rate limit middleware
│   ├── cache/
│   │   ├── redis.go              # Redis client wrapper
│   │   └── middleware.go         # Cache-aside middleware for REST routes
│   ├── gateway/
│   │   ├── router.go             # Chi router setup, route registration
│   │   ├── proxy.go              # Generic reverse proxy for REST passthrough
│   │   └── rest/
│   │       ├── github.go         # /api/v1/github/* handlers
│   │       └── weather.go        # /api/v1/weather/* handlers
│   ├── graph/
│   │   ├── schema.graphql        # GraphQL schema definition
│   │   ├── resolver.go           # Root resolver
│   │   └── resolvers/
│   │       ├── user.go           # User queries/mutations (hits Postgres)
│   │       ├── github.go         # GitHub queries (hits upstream + cache)
│   │       └── weather.go        # Weather queries (hits upstream + cache)
│   ├── db/
│   │   ├── postgres.go           # pgx connection pool setup
│   │   ├── migrations/           # SQL migration files
│   │   └── queries/              # Raw SQL queries (sqlc generated)
│   └── metrics/
│       └── prometheus.go         # Prometheus instrumentation
├── config/
│   └── config.go                 # Config struct loaded from env vars
├── docker-compose.yml            # Postgres + Redis + gateway
├── Dockerfile
├── .env.example
├── go.mod
├── go.sum
└── README.md
```

---

## GraphQL Schema

```graphql
type Query {
  # From Postgres
  user(id: ID!): User
  me: User                          # uses JWT claims

  # From GitHub API (cached)
  githubUser(login: String!): GitHubUser
  githubRepo(owner: String!, name: String!): GitHubRepo

  # From Open-Meteo (cached)
  weather(lat: Float!, lon: Float!): Weather
}

type Mutation {
  createUser(input: CreateUserInput!): User
  saveSearch(query: String!): SavedSearch
}

type User {
  id: ID!
  email: String!
  createdAt: String!
  savedSearches: [SavedSearch!]!
}

type SavedSearch {
  id: ID!
  query: String!
  createdAt: String!
}

type GitHubUser {
  login: String!
  name: String
  bio: String
  publicRepos: Int!
  followers: Int!
}

type GitHubRepo {
  name: String!
  description: String
  stars: Int!
  forks: Int!
  language: String
  openIssues: Int!
}

type Weather {
  latitude: Float!
  longitude: Float!
  temperature: Float!
  windspeed: Float!
  weatherCode: Int!
}
```

---

## Middleware Chain

Every request passes through this chain in order:

```
Request
  │
  ├─ 1. RequestID          — attach unique ID to context + response header
  ├─ 2. StructuredLogger   — log method, path, status, latency (zerolog)
  ├─ 3. Auth               — validate JWT or API key; attach claims to ctx
  ├─ 4. RateLimiter        — sliding window per client ID from claims
  ├─ 5. Cache              — check Redis before hitting upstream (REST only)
  │
  └─ Handler / Resolver
```

---

## Auth: JWT + API Keys

### JWT Flow
- Clients send `Authorization: Bearer <token>`
- Gateway validates signature using a shared secret (HS256) or public key (RS256)
- Claims (`sub`, `email`, `role`) are extracted and stored in request context
- Tokens are issued by a simple `/auth/token` endpoint (username+password against Postgres)

### API Key Flow
- Clients send `X-API-Key: <key>`
- Gateway looks up the key in a Postgres `api_keys` table
- Looks up the associated user and attaches them to context
- Keys are hashed (SHA-256) at rest — the plaintext is only shown once on creation

### Implementation notes
- Both methods hit the same context key, so all downstream middleware is auth-method-agnostic
- `/auth/*` routes are exempt from auth middleware
- Role field in claims gates access to admin-only resolvers

---

## Rate Limiting: Sliding Window

Algorithm: **sliding window log** backed by Redis sorted sets.

```
Key:   ratelimit:<client_id>
Value: sorted set of request timestamps (score = timestamp)
```

On each request:
1. Remove all entries older than the window (e.g. 60 seconds)
2. Count remaining entries
3. If count >= limit → return 429 with `Retry-After` header
4. Otherwise → add current timestamp and proceed

Limits (configurable via env):
- Authenticated users: 100 req/min
- API key clients: 500 req/min
- Unauthenticated (health/auth routes): no limit

---

## Caching: Redis Cache-Aside

Pattern: **cache-aside** (application manages cache explicitly).

```
Request hits cache middleware
  │
  ├─ Cache HIT  → return cached response, set X-Cache: HIT header
  │
  └─ Cache MISS → forward to upstream
                    │
                    └─ store response in Redis with TTL
                       return response, set X-Cache: MISS header
```

Cache keys:
- `cache:github:user:<login>` — TTL 5 minutes
- `cache:github:repo:<owner>:<name>` — TTL 5 minutes  
- `cache:weather:<lat>:<lon>` — TTL 10 minutes

Cache is **bypassed** for:
- Any mutating request (POST, PUT, DELETE, PATCH)
- GraphQL mutations
- Requests with `Cache-Control: no-cache` header

---

## REST Endpoints

These are passthrough/aggregation routes, mostly for direct API access without GraphQL:

```
POST   /auth/token              — issue JWT (email + password)
POST   /auth/apikey             — create API key (requires JWT)
DELETE /auth/apikey/:id         — revoke API key

GET    /api/v1/github/users/:login
GET    /api/v1/github/repos/:owner/:repo
GET    /api/v1/weather?lat=&lon=

GET    /health                  — liveness check
GET    /metrics                 — Prometheus metrics (internal only)
```

---

## Database Schema (Postgres)

```sql
CREATE TABLE users (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email      TEXT UNIQUE NOT NULL,
  password   TEXT NOT NULL,           -- bcrypt hash
  role       TEXT NOT NULL DEFAULT 'user',
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE api_keys (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID REFERENCES users(id) ON DELETE CASCADE,
  key_hash   TEXT UNIQUE NOT NULL,    -- SHA-256 of the plaintext key
  name       TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now(),
  last_used  TIMESTAMPTZ
);

CREATE TABLE saved_searches (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID REFERENCES users(id) ON DELETE CASCADE,
  query      TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now()
);
```

---

## Key Dependencies

| Package | Purpose |
|---|---|
| `go-chi/chi` | HTTP router (lightweight, middleware-friendly) |
| `graph-gophers/graphql-go` | GraphQL server |
| `redis/go-redis` | Redis client |
| `jackc/pgx` | Postgres driver (faster than `database/sql`) |
| `golang-jwt/jwt` | JWT parsing and validation |
| `rs/zerolog` | Structured JSON logging |
| `prometheus/client_golang` | Prometheus metrics |
| `golang.org/x/crypto` | bcrypt for password hashing |

---

## Observability

### Structured Logs (zerolog)
Every request logs:
```json
{
  "level": "info",
  "request_id": "abc-123",
  "method": "GET",
  "path": "/api/v1/github/users/torvalds",
  "status": 200,
  "latency_ms": 42,
  "client_id": "user-uuid",
  "cache": "HIT"
}
```

### Prometheus Metrics
- `nexus_requests_total` — counter by method, path, status
- `nexus_request_duration_seconds` — histogram by path
- `nexus_cache_hits_total` / `nexus_cache_misses_total`
- `nexus_ratelimit_rejected_total` — by client_id

---

## Docker Compose Setup

```yaml
services:
  gateway:
    build: .
    ports: ["8080:8080"]
    env_file: .env
    depends_on: [postgres, redis]

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: nexus
      POSTGRES_USER: nexus
      POSTGRES_PASSWORD: nexus
    volumes: ["pgdata:/var/lib/postgresql/data"]

  redis:
    image: redis:7-alpine
    command: redis-server --maxmemory 128mb --maxmemory-policy allkeys-lru

volumes:
  pgdata:
```

---

## Environment Variables

```bash
# Server
PORT=8080
ENV=development

# Auth
JWT_SECRET=your-secret-here
JWT_EXPIRY_MINUTES=60

# Postgres
DATABASE_URL=postgres://nexus:nexus@localhost:5432/nexus

# Redis
REDIS_URL=redis://localhost:6379

# Upstream APIs
GITHUB_TOKEN=ghp_...

# Rate limits
RATE_LIMIT_AUTHED=100        # req/min for JWT users
RATE_LIMIT_APIKEY=500        # req/min for API key clients
RATE_LIMIT_WINDOW_SECONDS=60
```

---

## Build Order for Claude Code

Implement in this order to avoid dependency issues:

1. **Scaffolding** — `go mod init`, directory structure, `config.go`, env loading
2. **Database** — Postgres connection, migrations, user/apikey/savedsearch queries
3. **Auth** — JWT issuance + validation, API key creation + lookup, middleware
4. **Redis** — client wrapper, cache middleware
5. **Rate limiter** — sliding window implementation, middleware
6. **REST handlers** — GitHub and weather upstream calls with cache integration
7. **GraphQL** — schema, resolvers wired to DB + upstream handlers
8. **Router** — chi setup, middleware chain, route registration
9. **Metrics** — Prometheus instrumentation across all layers
10. **Docker** — Dockerfile, docker-compose, `.env.example`
11. **README** — architecture diagram, setup instructions, example queries, benchmark results

---

## What to Put in the README (for CV impact)

- Architecture diagram (can use Mermaid)
- Design decisions section: why sliding window over token bucket, why cache-aside over write-through, why chi over gin
- Benchmark table: requests/sec with and without cache, latency percentiles (p50/p95/p99) — run with `k6` or `hey`
- Example GraphQL queries with responses
- "What I'd add next" section: circuit breaker (with `sony/gobreaker`), request coalescing for cache stampede prevention, gRPC upstream support
