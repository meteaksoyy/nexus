package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/user/nexus/internal/db"
	"github.com/user/nexus/internal/db/queries"
)

// RefreshService handles refresh token issuance and rotation.
type RefreshService struct {
	q          *queries.RefreshTokenQueries
	refreshDays int
}

func NewRefreshService(q *queries.RefreshTokenQueries, refreshDays int) *RefreshService {
	return &RefreshService{q: q, refreshDays: refreshDays}
}

// Issue generates a new opaque refresh token, stores a SHA-256 hash in the DB,
// and returns the plaintext token (shown only once).
func (s *RefreshService) Issue(ctx context.Context, userID string) (string, error) {
	plaintext := uuid.New().String()
	hash := hashToken(plaintext)
	expiresAt := time.Now().Add(time.Duration(s.refreshDays) * 24 * time.Hour)

	if _, err := s.q.Create(ctx, userID, hash, expiresAt); err != nil {
		return "", fmt.Errorf("issue refresh token: %w", err)
	}
	return plaintext, nil
}

// Rotate validates a refresh token, revokes it, and issues a new one (token rotation).
// Returns the stored token record and its userID so the caller can issue a new JWT.
func (s *RefreshService) Rotate(ctx context.Context, plaintext string) (db.RefreshToken, string, error) {
	hash := hashToken(plaintext)
	rt, err := s.q.GetByHash(ctx, hash)
	if err != nil {
		return db.RefreshToken{}, "", errors.New("invalid refresh token")
	}
	if rt.Revoked {
		return db.RefreshToken{}, "", errors.New("refresh token already revoked")
	}
	if time.Now().After(rt.ExpiresAt) {
		return db.RefreshToken{}, "", errors.New("refresh token expired")
	}

	// Revoke the used token (rotation — each token is single-use)
	if err := s.q.Revoke(ctx, rt.ID); err != nil {
		return db.RefreshToken{}, "", fmt.Errorf("revoke old token: %w", err)
	}

	// Issue replacement
	newPlaintext, err := s.Issue(ctx, rt.UserID)
	if err != nil {
		return db.RefreshToken{}, "", err
	}

	return rt, newPlaintext, nil
}

// Revoke invalidates a refresh token by hash.
func (s *RefreshService) Revoke(ctx context.Context, plaintext string) error {
	hash := hashToken(plaintext)
	rt, err := s.q.GetByHash(ctx, hash)
	if err != nil {
		return nil // already gone — idempotent
	}
	return s.q.Revoke(ctx, rt.ID)
}

func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
