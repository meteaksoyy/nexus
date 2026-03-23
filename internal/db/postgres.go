package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/user/nexus/config"
)

// NewPool creates a pgx connection pool and verifies connectivity.
func NewPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pg pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}
