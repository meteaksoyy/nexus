package queries

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/meteaksoyy/nexus/internal/db"
)

type UserQueries struct{ pool *pgxpool.Pool }

func NewUserQueries(pool *pgxpool.Pool) *UserQueries {
	return &UserQueries{pool: pool}
}

func (q *UserQueries) CreateUser(ctx context.Context, email, passwordHash, role string) (db.User, error) {
	const sql = `
		INSERT INTO users (email, password_hash, role)
		VALUES ($1, $2, $3)
		RETURNING id, email, password_hash, role, created_at`

	var u db.User
	err := q.pool.QueryRow(ctx, sql, email, passwordHash, role).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return db.User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

func (q *UserQueries) GetUserByEmail(ctx context.Context, email string) (db.User, error) {
	const sql = `
		SELECT id, email, password_hash, role, created_at
		FROM users WHERE email = $1`

	var u db.User
	err := q.pool.QueryRow(ctx, sql, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return db.User{}, fmt.Errorf("get user by email: %w", err)
	}
	return u, nil
}

func (q *UserQueries) GetUserByID(ctx context.Context, id string) (db.User, error) {
	const sql = `
		SELECT id, email, password_hash, role, created_at
		FROM users WHERE id = $1`

	var u db.User
	err := q.pool.QueryRow(ctx, sql, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return db.User{}, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}
