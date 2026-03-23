package db

import "time"

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

type APIKey struct {
	ID        string
	UserID    string
	KeyHash   string
	Name      string
	CreatedAt time.Time
	LastUsed  *time.Time
}

type SavedSearch struct {
	ID        string
	UserID    string
	Query     string
	CreatedAt time.Time
}

type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	Revoked   bool
	CreatedAt time.Time
}
