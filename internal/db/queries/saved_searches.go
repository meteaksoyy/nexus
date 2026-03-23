package queries

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/user/nexus/internal/db"
)

type SavedSearchQueries struct{ pool *pgxpool.Pool }

func NewSavedSearchQueries(pool *pgxpool.Pool) *SavedSearchQueries {
	return &SavedSearchQueries{pool: pool}
}

func (q *SavedSearchQueries) CreateSavedSearch(ctx context.Context, userID, query string) (db.SavedSearch, error) {
	const sql = `
		INSERT INTO saved_searches (user_id, query)
		VALUES ($1, $2)
		RETURNING id, user_id, query, created_at`

	var s db.SavedSearch
	err := q.pool.QueryRow(ctx, sql, userID, query).
		Scan(&s.ID, &s.UserID, &s.Query, &s.CreatedAt)
	if err != nil {
		return db.SavedSearch{}, fmt.Errorf("create saved search: %w", err)
	}
	return s, nil
}

func (q *SavedSearchQueries) GetSavedSearchesByUserID(ctx context.Context, userID string) ([]db.SavedSearch, error) {
	const sql = `
		SELECT id, user_id, query, created_at
		FROM saved_searches WHERE user_id = $1
		ORDER BY created_at DESC`

	rows, err := q.pool.Query(ctx, sql, userID)
	if err != nil {
		return nil, fmt.Errorf("list saved searches: %w", err)
	}
	defer rows.Close()

	var searches []db.SavedSearch
	for rows.Next() {
		var s db.SavedSearch
		if err := rows.Scan(&s.ID, &s.UserID, &s.Query, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan saved search: %w", err)
		}
		searches = append(searches, s)
	}
	return searches, rows.Err()
}

// GetSavedSearchesByUserIDs batches a lookup for multiple user IDs in a single query.
// Used by the DataLoader to solve the N+1 problem.
func (q *SavedSearchQueries) GetSavedSearchesByUserIDs(ctx context.Context, userIDs []string) (map[string][]db.SavedSearch, error) {
	const sql = `
		SELECT id, user_id, query, created_at
		FROM saved_searches WHERE user_id = ANY($1)
		ORDER BY created_at DESC`

	rows, err := q.pool.Query(ctx, sql, userIDs)
	if err != nil {
		return nil, fmt.Errorf("batch list saved searches: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]db.SavedSearch, len(userIDs))
	for rows.Next() {
		var s db.SavedSearch
		if err := rows.Scan(&s.ID, &s.UserID, &s.Query, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan saved search: %w", err)
		}
		result[s.UserID] = append(result[s.UserID], s)
	}
	return result, rows.Err()
}
