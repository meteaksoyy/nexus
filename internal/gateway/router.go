package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"

	"github.com/meteaksoyy/nexus/config"
	"github.com/meteaksoyy/nexus/internal/auth"
	"github.com/meteaksoyy/nexus/internal/cache"
	"github.com/meteaksoyy/nexus/internal/gateway/rest"
	graph "github.com/meteaksoyy/nexus/internal/graph"
	"github.com/meteaksoyy/nexus/internal/graph/dataloader"
	"github.com/meteaksoyy/nexus/internal/graph/resolvers"
	"github.com/meteaksoyy/nexus/internal/ibkr"
	"github.com/meteaksoyy/nexus/internal/metrics"
	"github.com/meteaksoyy/nexus/internal/ratelimit"
	"github.com/meteaksoyy/nexus/internal/upstream"

	"github.com/redis/go-redis/v9"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/meteaksoyy/nexus/internal/db/queries"
	dbauth "github.com/meteaksoyy/nexus/internal/auth"
)

// NewRouter wires together all middleware and handlers into a chi router.
func NewRouter(
	cfg *config.Config,
	pool *pgxpool.Pool,
	rdb *redis.Client,
	ibkrClient *ibkr.Client,
	log zerolog.Logger,
) http.Handler {
	r := chi.NewRouter()

	// ── Global middleware ────────────────────────────────────────────────────
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(requestLogger(log))
	r.Use(chimw.Recoverer)
	r.Use(otelMiddleware())
	r.Use(metrics.Middleware())

	// ── Dependencies ─────────────────────────────────────────────────────────
	userQ := queries.NewUserQueries(pool)
	apiKeyQ := queries.NewAPIKeyQueries(pool)
	saveQ := queries.NewSavedSearchQueries(pool)
	refreshQ := queries.NewRefreshTokenQueries(pool)

	denylist := auth.NewDenylist(rdb)
	refreshSvc := auth.NewRefreshService(refreshQ)
	apiKeySvc := auth.NewAPIKeyService(apiKeyQ)

	authHandlers := auth.NewHandlers(userQ, apiKeySvc, refreshSvc, denylist, cfg, log)

	cacheMw := cache.NewMiddleware(rdb)
	sw := ratelimit.NewSlidingWindow(rdb, cfg.RateLimitAuthed, cfg.RateLimitWindow)

	githubClient := upstream.New("github", nil)
	githubHandlers := rest.NewGitHubHandlers(githubClient, cfg.GithubToken, log)
	ibkrHandlers := rest.NewIBKRHandlers(ibkrClient, log)

	// ── Auth routes (no auth middleware) ─────────────────────────────────────
	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", authHandlers.Register)
		r.Post("/token", authHandlers.Token)
		r.Post("/refresh", authHandlers.Refresh)
		r.Post("/logout", auth.Middleware(cfg, denylist)(http.HandlerFunc(authHandlers.Logout)).ServeHTTP)
		r.Post("/apikey", auth.Middleware(cfg, denylist)(http.HandlerFunc(authHandlers.CreateAPIKey)).ServeHTTP)
		r.Delete("/apikey/{id}", auth.Middleware(cfg, denylist)(http.HandlerFunc(authHandlers.DeleteAPIKey)).ServeHTTP)
	})

	// ── Authenticated API routes ──────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(dbauth.Middleware(cfg, denylist))
		r.Use(ratelimit.Middleware(cfg, sw))

		// GitHub endpoints (cached)
		r.Route("/api/v1/github", func(r chi.Router) {
			r.With(cacheMw.Handler(5*time.Minute, func(req *http.Request) string {
				return "cache:github:user:" + chi.URLParam(req, "login")
			})).Get("/users/{login}", githubHandlers.GetUser)

			r.With(cacheMw.Handler(5*time.Minute, func(req *http.Request) string {
				return "cache:github:repo:" + chi.URLParam(req, "owner") + ":" + chi.URLParam(req, "repo")
			})).Get("/repos/{owner}/{repo}", githubHandlers.GetRepo)
		})

		// IBKR market data endpoints (cached)
		r.Route("/api/v1/market", func(r chi.Router) {
			r.With(cacheMw.Handler(30*time.Second, func(req *http.Request) string {
				return "cache:ibkr:quote:" + req.URL.Query().Get("symbol")
			})).Get("/quote", ibkrHandlers.Quote)

			r.With(cacheMw.Handler(5*time.Minute, func(req *http.Request) string {
				q := req.URL.Query()
				return "cache:ibkr:history:" + q.Get("symbol") + ":" + q.Get("period") + ":" + q.Get("bar")
			})).Get("/history", ibkrHandlers.History)

			r.With(cacheMw.Handler(time.Hour, func(req *http.Request) string {
				return "cache:ibkr:contract:" + req.URL.Query().Get("symbol")
			})).Get("/search", ibkrHandlers.Search)
		})

		// GraphQL endpoint
		schema := buildSchema(cfg, pool, ibkrClient, log)
		r.With(dataloaderMiddleware(saveQ)).Handle("/graphql", &relay.Handler{Schema: schema})
	})

	// ── Observability ─────────────────────────────────────────────────────────
	r.Get("/health", healthHandler)
	r.Handle("/metrics", metrics.Handler())

	return r
}

func buildSchema(cfg *config.Config, pool *pgxpool.Pool, ibkrClient *ibkr.Client, log zerolog.Logger) *graphql.Schema {
	userQ := queries.NewUserQueries(pool)
	apiKeyQ := queries.NewAPIKeyQueries(pool)
	saveQ := queries.NewSavedSearchQueries(pool)
	apiKeySvc := auth.NewAPIKeyService(apiKeyQ)

	githubClient := upstream.New("github", nil)

	userRes := resolvers.NewUserResolver(userQ, saveQ, apiKeySvc, log)
	githubRes := resolvers.NewGitHubResolver(githubClient, cfg.GithubToken, log)
	ibkrRes := resolvers.NewIBKRResolver(ibkrClient, log)

	root := graph.NewRootResolver(userRes, githubRes, ibkrRes)

	schemaBytes := mustReadSchema()
	schema := graphql.MustParseSchema(string(schemaBytes), root, graph.SchemaOpts()...)
	return schema
}

func dataloaderMiddleware(saveQ *queries.SavedSearchQueries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			loader := dataloader.New(saveQ)
			ctx := dataloader.WithLoader(r.Context(), loader)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func otelMiddleware() func(http.Handler) http.Handler {
	tracer := otel.Tracer("nexus")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path)
			defer span.End()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requestLogger(log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			log.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Dur("latency", time.Since(start)).
				Str("request_id", chimw.GetReqID(r.Context())).
				Msg("request")
		})
	}
}
