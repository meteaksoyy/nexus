package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Middleware validates incoming requests via JWT or API key.
type Middleware struct {
	secret     string
	denylist   *Denylist
	apiKeySvc  *APIKeyService
	log        zerolog.Logger
}

func NewMiddleware(secret string, denylist *Denylist, apiKeySvc *APIKeyService, log zerolog.Logger) *Middleware {
	return &Middleware{
		secret:    secret,
		denylist:  denylist,
		apiKeySvc: apiKeySvc,
		log:       log,
	}
}

// Handler is the chi middleware function. It requires authentication on every request.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Try JWT first
		if bearer := extractBearer(r); bearer != "" {
			claims, err := ValidateJWT(m.secret, bearer)
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}

			// Check denylist (logout / revocation)
			revoked, err := m.denylist.IsRevoked(ctx, claims.JTI)
			if err != nil {
				m.log.Error().Err(err).Msg("denylist check failed")
				// Fail open on Redis error to avoid locking out users during outages.
				// Adjust to fail closed if security requirements demand it.
			} else if revoked {
				http.Error(w, "token revoked", http.StatusUnauthorized)
				return
			}

			ctx = setAuthContext(ctx, claims.UserID, claims.Email, claims.Role, claims.JTI, claims.ExpiresAt.Time, ClientTypeJWT)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Try API key
		if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
			_, user, err := m.apiKeySvc.Validate(ctx, apiKey)
			if err != nil {
				http.Error(w, "invalid api key", http.StatusUnauthorized)
				return
			}

			ctx = setAuthContext(ctx, user.ID, user.Email, user.Role, "", time.Time{}, ClientTypeAPIKey)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		http.Error(w, "authentication required", http.StatusUnauthorized)
	})
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
