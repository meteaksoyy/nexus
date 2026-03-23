package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims represents the JWT payload.
type Claims struct {
	UserID string `json:"sub"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	JTI    string `json:"jti"` // unique token ID — used for revocation
	jwt.RegisteredClaims
}

// IssueJWT signs a new short-lived JWT for the given user.
func IssueJWT(secret string, expiryMinutes int, userID, email, role string) (string, Claims, error) {
	now := time.Now()
	jti := uuid.New().String()
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		JTI:    jti,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expiryMinutes) * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	return signed, claims, err
}

// ValidateJWT parses and validates a JWT, returning its claims.
func ValidateJWT(secret, tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}
