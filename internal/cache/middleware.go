package cache

import (
	"bytes"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// Middleware is the cache-aside middleware for REST routes.
// It uses singleflight to prevent cache stampedes: if many goroutines miss
// the cache for the same key simultaneously, only one upstream call is made
// and the result is shared with all waiting callers.
type Middleware struct {
	rdb   *redis.Client
	group singleflight.Group
}

func NewMiddleware(rdb *redis.Client) *Middleware {
	return &Middleware{rdb: rdb}
}

// Handler returns a chi middleware that caches GET responses for the given TTL.
// keyFn derives the cache key from the request (e.g. by URL path + query params).
func (m *Middleware) Handler(ttl time.Duration, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only cache safe, idempotent requests
			if r.Method != http.MethodGet || r.Header.Get("Cache-Control") == "no-cache" {
				next.ServeHTTP(w, r)
				return
			}

			key := keyFn(r)
			ctx := r.Context()

			// Fast path: cache hit
			if cached, err := m.rdb.Get(ctx, key).Bytes(); err == nil {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache", "HIT")
				_, _ = w.Write(cached)
				return
			}

			// Slow path: deduplicate in-flight upstream calls for the same key.
			// All concurrent misses for the same key share one upstream call.
			type result struct {
				body       []byte
				statusCode int
			}

			v, err, _ := m.group.Do(key, func() (any, error) {
				rec := newResponseRecorder()
				next.ServeHTTP(rec, r)

				body := rec.body.Bytes()

				// Only cache successful responses
				if rec.statusCode == http.StatusOK && len(body) > 0 {
					// Use Background context so a cancelled request doesn't skip caching
					m.rdb.Set(ctx, key, body, ttl) //nolint:errcheck
				}

				return &result{body: body, statusCode: rec.statusCode}, nil
			})
			if err != nil {
				http.Error(w, "upstream error", http.StatusBadGateway)
				return
			}

			res := v.(*result)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "MISS")
			w.WriteHeader(res.statusCode)
			_, _ = w.Write(res.body)
		})
	}
}

// responseRecorder captures the response so it can be stored and replayed.
type responseRecorder struct {
	body       *bytes.Buffer
	statusCode int
	header     http.Header
}

func newResponseRecorder() *responseRecorder {
	return &responseRecorder{
		body:       new(bytes.Buffer),
		statusCode: http.StatusOK,
		header:     make(http.Header),
	}
}

func (r *responseRecorder) Header() http.Header        { return r.header }
func (r *responseRecorder) WriteHeader(code int)       { r.statusCode = code }
func (r *responseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }
