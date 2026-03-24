package auth

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const testSecret = "test-secret-key"

// ---- JWT ----

func TestIssueAndValidateJWT(t *testing.T) {
	tokenStr, claims, err := IssueJWT(testSecret, 60, "user-1", "test@example.com", "user")
	if err != nil {
		t.Fatalf("IssueJWT: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected non-empty token string")
	}
	if claims.UserID != "user-1" {
		t.Errorf("UserID: got %q, want %q", claims.UserID, "user-1")
	}
	if claims.JTI == "" {
		t.Error("expected non-empty JTI")
	}

	got, err := ValidateJWT(testSecret, tokenStr)
	if err != nil {
		t.Fatalf("ValidateJWT: %v", err)
	}
	if got.UserID != "user-1" {
		t.Errorf("validated UserID: got %q, want %q", got.UserID, "user-1")
	}
	if got.Email != "test@example.com" {
		t.Errorf("validated Email: got %q, want %q", got.Email, "test@example.com")
	}
	if got.Role != "user" {
		t.Errorf("validated Role: got %q, want %q", got.Role, "user")
	}
}

func TestValidateJWT_WrongSecret(t *testing.T) {
	tokenStr, _, err := IssueJWT(testSecret, 60, "user-1", "test@example.com", "user")
	if err != nil {
		t.Fatalf("IssueJWT: %v", err)
	}
	_, err = ValidateJWT("wrong-secret", tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestValidateJWT_Expired(t *testing.T) {
	// Issue a token that expires in the past by using -1 minutes
	tokenStr, _, err := IssueJWT(testSecret, -1, "user-1", "test@example.com", "user")
	if err != nil {
		t.Fatalf("IssueJWT: %v", err)
	}
	_, err = ValidateJWT(testSecret, tokenStr)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateJWT_Malformed(t *testing.T) {
	_, err := ValidateJWT(testSecret, "not.a.token")
	if err == nil {
		t.Fatal("expected error for malformed token, got nil")
	}
}

// ---- Denylist ----

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestDenylist_RevokeAndCheck(t *testing.T) {
	rdb := newTestRedis(t)
	dl := NewDenylist(rdb)
	ctx := t.Context()

	tokenStr, claims, err := IssueJWT(testSecret, 60, "user-1", "test@example.com", "user")
	if err != nil {
		t.Fatalf("IssueJWT: %v", err)
	}
	_ = tokenStr

	// Not revoked before Revoke is called
	revoked, err := dl.IsRevoked(ctx, claims.JTI)
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("token should not be revoked yet")
	}

	// Revoke it
	expiresAt := time.Now().Add(60 * time.Minute)
	if err := dl.Revoke(ctx, claims.JTI, expiresAt); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Now it should be revoked
	revoked, err = dl.IsRevoked(ctx, claims.JTI)
	if err != nil {
		t.Fatalf("IsRevoked after revoke: %v", err)
	}
	if !revoked {
		t.Fatal("token should be revoked")
	}
}

func TestDenylist_AlreadyExpired(t *testing.T) {
	rdb := newTestRedis(t)
	dl := NewDenylist(rdb)
	ctx := t.Context()

	// Revoking an already-expired token is a no-op — should not error
	err := dl.Revoke(ctx, "some-jti", time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("Revoke of expired token should be no-op, got: %v", err)
	}

	revoked, err := dl.IsRevoked(ctx, "some-jti")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("expired token revocation should not write to Redis")
	}
}
