package auth

import (
	"context"
	"time"
)

type contextKey string

const (
	ctxKeyUserID     = contextKey("user_id")
	ctxKeyEmail      = contextKey("email")
	ctxKeyRole       = contextKey("role")
	ctxKeyJTI        = contextKey("jti")
	ctxKeyClientType = contextKey("client_type")
	ctxKeyExpiresAt  = contextKey("expires_at")
)

// ClientType distinguishes how a request was authenticated (affects rate limit tier).
type ClientType string

const (
	ClientTypeJWT    ClientType = "jwt"
	ClientTypeAPIKey ClientType = "apikey"
)

func setAuthContext(ctx context.Context, userID, email, role, jti string, expiresAt time.Time, ct ClientType) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUserID, userID)
	ctx = context.WithValue(ctx, ctxKeyEmail, email)
	ctx = context.WithValue(ctx, ctxKeyRole, role)
	ctx = context.WithValue(ctx, ctxKeyJTI, jti)
	ctx = context.WithValue(ctx, ctxKeyExpiresAt, expiresAt)
	ctx = context.WithValue(ctx, ctxKeyClientType, ct)
	return ctx
}

// UserIDFromContext extracts the authenticated user ID.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUserID).(string)
	return v
}

// EmailFromContext extracts the authenticated user's email.
func EmailFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyEmail).(string)
	return v
}

// RoleFromContext extracts the authenticated user's role.
func RoleFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRole).(string)
	return v
}

// JTIFromContext extracts the JWT ID (used for revocation).
func JTIFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyJTI).(string)
	return v
}

// ExpiresAtFromContext extracts the JWT expiry time (used when revoking).
func ExpiresAtFromContext(ctx context.Context) time.Time {
	v, _ := ctx.Value(ctxKeyExpiresAt).(time.Time)
	return v
}

// ClientTypeFromContext returns how the request was authenticated.
func ClientTypeFromContext(ctx context.Context) ClientType {
	v, _ := ctx.Value(ctxKeyClientType).(ClientType)
	return v
}
