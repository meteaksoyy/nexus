package queries

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/meteaksoyy/nexus/internal/db"
)

type RefreshTokenQueries struct{ pool *pgxpool.Pool }

func NewRefreshTokenQueries(pool *pgxpool.Pool) *RefreshTokenQueries {
	return &RefreshTokenQueries{pool: pool}
}

func (q *RefreshTokenQueries) Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (db.RefreshToken, error) {
	const sql = `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, token_hash, expires_at, revoked, created_at`

	var t db.RefreshToken
	err := q.pool.QueryRow(ctx, sql, userID, tokenHash, expiresAt).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.Revoked, &t.CreatedAt)
	if err != nil {
		return db.RefreshToken{}, fmt.Errorf("create refresh token: %w", err)
	}
	return t, nil
}

func (q *RefreshTokenQueries) GetByHash(ctx context.Context, tokenHash string) (db.RefreshToken, error) {
	const sql = `
		SELECT id, user_id, token_hash, expires_at, revoked, created_at
		FROM refresh_tokens WHERE token_hash = $1`

	var t db.RefreshToken
	err := q.pool.QueryRow(ctx, sql, tokenHash).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.Revoked, &t.CreatedAt)
	if err != nil {
		return db.RefreshToken{}, fmt.Errorf("get refresh token: %w", err)
	}
	return t, nil
}

// Revoke marks a refresh token as revoked.
func (q *RefreshTokenQueries) Revoke(ctx context.Context, id string) error {
	const sql = `UPDATE refresh_tokens SET revoked = true WHERE id = $1`
	_, err := q.pool.Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

// RevokeAllForUser revokes all refresh tokens for a user (useful for logout-all).
func (q *RefreshTokenQueries) RevokeAllForUser(ctx context.Context, userID string) error {
	const sql = `UPDATE refresh_tokens SET revoked = true WHERE user_id = $1 AND revoked = false`
	_, err := q.pool.Exec(ctx, sql, userID)
	if err != nil {
		return fmt.Errorf("revoke all refresh tokens: %w", err)
	}
	return nil
}
