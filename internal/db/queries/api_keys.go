package queries

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/user/nexus/internal/db"
)

type APIKeyQueries struct{ pool *pgxpool.Pool }

func NewAPIKeyQueries(pool *pgxpool.Pool) *APIKeyQueries {
	return &APIKeyQueries{pool: pool}
}

func (q *APIKeyQueries) CreateAPIKey(ctx context.Context, userID, keyHash, name string) (db.APIKey, error) {
	const sql = `
		INSERT INTO api_keys (user_id, key_hash, name)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, key_hash, name, created_at, last_used`

	var k db.APIKey
	err := q.pool.QueryRow(ctx, sql, userID, keyHash, name).
		Scan(&k.ID, &k.UserID, &k.KeyHash, &k.Name, &k.CreatedAt, &k.LastUsed)
	if err != nil {
		return db.APIKey{}, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}

// GetAPIKeyByHash returns the key and its associated user. Updates last_used timestamp.
func (q *APIKeyQueries) GetAPIKeyByHash(ctx context.Context, hash string) (db.APIKey, db.User, error) {
	const sql = `
		UPDATE api_keys SET last_used = now()
		WHERE key_hash = $1
		RETURNING id, user_id, key_hash, name, created_at, last_used`

	var k db.APIKey
	err := q.pool.QueryRow(ctx, sql, hash).
		Scan(&k.ID, &k.UserID, &k.KeyHash, &k.Name, &k.CreatedAt, &k.LastUsed)
	if err != nil {
		return db.APIKey{}, db.User{}, fmt.Errorf("get api key: %w", err)
	}

	const userSQL = `SELECT id, email, password_hash, role, created_at FROM users WHERE id = $1`
	var u db.User
	err = q.pool.QueryRow(ctx, userSQL, k.UserID).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return db.APIKey{}, db.User{}, fmt.Errorf("get api key user: %w", err)
	}

	return k, u, nil
}

func (q *APIKeyQueries) DeleteAPIKey(ctx context.Context, id, userID string) error {
	const sql = `DELETE FROM api_keys WHERE id = $1 AND user_id = $2`
	tag, err := q.pool.Exec(ctx, sql, id, userID)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key not found or not owned by user")
	}
	return nil
}
