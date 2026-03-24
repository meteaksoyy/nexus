package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/meteaksoyy/nexus/internal/db"
	"github.com/meteaksoyy/nexus/internal/db/queries"
)

// APIKeyService handles creation and validation of API keys.
type APIKeyService struct {
	q *queries.APIKeyQueries
}

func NewAPIKeyService(q *queries.APIKeyQueries) *APIKeyService {
	return &APIKeyService{q: q}
}

// Create generates a new API key, stores a SHA-256 hash, and returns the plaintext (shown once).
func (s *APIKeyService) Create(ctx context.Context, userID, name string) (string, db.APIKey, error) {
	plaintext := uuid.New().String()
	hash := hashAPIKey(plaintext)

	key, err := s.q.CreateAPIKey(ctx, userID, hash, name)
	if err != nil {
		return "", db.APIKey{}, fmt.Errorf("create api key: %w", err)
	}
	return plaintext, key, nil
}

// Validate looks up an API key by its hash and returns the associated user.
func (s *APIKeyService) Validate(ctx context.Context, plaintext string) (db.APIKey, db.User, error) {
	hash := hashAPIKey(plaintext)
	return s.q.GetAPIKeyByHash(ctx, hash)
}

// Delete removes an API key by ID, verifying ownership by userID.
func (s *APIKeyService) Delete(ctx context.Context, id, userID string) error {
	return s.q.DeleteAPIKey(ctx, id, userID)
}

func hashAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
