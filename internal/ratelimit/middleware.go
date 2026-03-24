package ratelimit

import (
	"fmt"
	"net/http"
	"time"

	"github.com/meteaksoyy/nexus/config"
	"github.com/meteaksoyy/nexus/internal/auth"
)

// Middleware returns a chi-compatible rate limiting middleware.
// Limits are tiered by client type:
//   - JWT clients:    cfg.RateLimitAuthed  req / cfg.RateLimitWindow
//   - API key clients: cfg.RateLimitAPIKey req / cfg.RateLimitWindow
//
// Clients that exceed their limit receive a 429 with a Retry-After header.
// The rate limiter is bypassed for unauthenticated contexts (e.g. /auth/* routes
// should be mounted outside the auth middleware, so this case is a safety net).
func Middleware(cfg *config.Config, sw *SlidingWindow) func(http.Handler) http.Handler {
	authedSW := NewSlidingWindow(sw.rdb, cfg.RateLimitAuthed, cfg.RateLimitWindow)
	apiKeySW := NewSlidingWindow(sw.rdb, cfg.RateLimitAPIKey, cfg.RateLimitWindow)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			clientID := auth.UserIDFromContext(ctx)
			if clientID == "" {
				// No auth context — skip rate limiting (unauthenticated public routes)
				next.ServeHTTP(w, r)
				return
			}

			var limiter *SlidingWindow
			switch auth.ClientTypeFromContext(ctx) {
			case auth.ClientTypeAPIKey:
				limiter = apiKeySW
			default:
				limiter = authedSW
			}

			allowed, remaining, retryAfter, err := limiter.Allow(ctx, clientID)
			if err != nil {
				// Redis error — fail open with a log-worthy warning
				// (don't block the request because of an infrastructure glitch)
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limiter.limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

			if !allowed {
				secs := int(retryAfter / time.Second)
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%d", secs))
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
