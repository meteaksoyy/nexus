//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/meteaksoyy/nexus/config"
	"github.com/meteaksoyy/nexus/internal/gateway"
)

// testEnv holds all resources for an integration test run.
type testEnv struct {
	server *httptest.Server
	cfg    *config.Config
	pool   *pgxpool.Pool
	rdb    *redis.Client
}

// newTestEnv spins up Postgres and Redis via testcontainers, runs migrations,
// and starts the full nexus HTTP server on a random port.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()

	// ── Postgres ──────────────────────────────────────────────────────────────
	pgContainer, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:16-alpine"),
		postgres.WithDatabase("nexus_test"),
		postgres.WithUsername("nexus"),
		postgres.WithPassword("nexus"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { pgContainer.Terminate(ctx) }) //nolint:errcheck

	pgURL, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, pgURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := runMigrations(ctx, pool); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	// ── Redis ─────────────────────────────────────────────────────────────────
	redisContainer, err := redismod.RunContainer(ctx,
		testcontainers.WithImage("redis:7-alpine"),
	)
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	t.Cleanup(func() { redisContainer.Terminate(ctx) }) //nolint:errcheck

	redisURL, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("redis connection string: %v", err)
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { rdb.Close() }) //nolint:errcheck

	// ── Config ────────────────────────────────────────────────────────────────
	cfg := &config.Config{
		Port:              "0",
		Env:               "test",
		JWTSecret:         "test-integration-secret",
		JWTExpiryMinutes:  60,
		JWTRefreshDays:    7,
		DatabaseURL:       pgURL,
		RedisURL:          redisURL,
		GithubToken:       "test-token",
		RateLimitAuthed:   100,
		RateLimitAPIKey:   500,
		RateLimitWindow:   time.Minute,
		OTELEndpoint:      "",
		IBKRGatewayURL:    "",
		IBKRUsername:      "",
		IBKRPassword:      "",
	}

	log := zerolog.Nop()
	router := gateway.NewRouter(cfg, pool, rdb, nil, log)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return &testEnv{
		server: server,
		cfg:    cfg,
		pool:   pool,
		rdb:    rdb,
	}
}

func (e *testEnv) url(path string) string {
	return e.server.URL + path
}

// runMigrations applies all SQL migrations directly against the pool.
func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			email TEXT UNIQUE NOT NULL,
			password TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'user',
			created_at TIMESTAMPTZ DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			key_hash TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT now(),
			last_used TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS saved_searches (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			query TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT UNIQUE NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			revoked BOOLEAN DEFAULT false,
			created_at TIMESTAMPTZ DEFAULT now()
		)`,
	}

	for _, m := range migrations {
		if _, err := pool.Exec(ctx, m); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}
	return nil
}
