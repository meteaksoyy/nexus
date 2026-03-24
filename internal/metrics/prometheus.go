package metrics

import (
	"net/http"
	"strconv"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nexus_requests_total",
		Help: "Total HTTP requests by method, path, and status.",
	}, []string{"method", "path", "status"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nexus_request_duration_seconds",
		Help:    "HTTP request latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	cacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nexus_cache_hits_total",
		Help: "Total Redis cache hits.",
	}, []string{"key_prefix"})

	cacheMisses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nexus_cache_misses_total",
		Help: "Total Redis cache misses.",
	}, []string{"key_prefix"})

	rateLimitRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nexus_ratelimit_rejected_total",
		Help: "Total requests rejected by the rate limiter.",
	}, []string{"client_id"})

	circuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nexus_circuit_breaker_state",
		Help: "Circuit breaker state per upstream (0=closed, 1=open, 2=half-open).",
	}, []string{"upstream"})

	graphqlRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nexus_graphql_rejected_total",
		Help: "Total GraphQL queries rejected by complexity/depth limits.",
	}, []string{"reason"})
)

// Middleware records per-request metrics (total count + latency histogram).
func Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			status := strconv.Itoa(ww.Status())
			requestsTotal.WithLabelValues(r.Method, r.URL.Path, status).Inc()
			requestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(time.Since(start).Seconds())
		})
	}
}

// Handler returns the Prometheus HTTP handler for /metrics.
func Handler() http.Handler {
	return promhttp.Handler()
}

// RecordCacheHit increments the cache hit counter for a given key prefix.
func RecordCacheHit(keyPrefix string) {
	cacheHits.WithLabelValues(keyPrefix).Inc()
}

// RecordCacheMiss increments the cache miss counter for a given key prefix.
func RecordCacheMiss(keyPrefix string) {
	cacheMisses.WithLabelValues(keyPrefix).Inc()
}

// RecordRateLimitRejection increments the rate limit rejection counter.
func RecordRateLimitRejection(clientID string) {
	rateLimitRejected.WithLabelValues(clientID).Inc()
}

// RecordCircuitBreakerState updates the gauge for an upstream's circuit state.
// stateStr should be "closed", "open", or "half-open".
func RecordCircuitBreakerState(upstream, stateStr string) {
	var val float64
	switch stateStr {
	case "open":
		val = 1
	case "half-open":
		val = 2
	}
	circuitBreakerState.WithLabelValues(upstream).Set(val)
}

// RecordGraphQLRejection increments the GraphQL rejection counter.
// reason should be "complexity" or "depth".
func RecordGraphQLRejection(reason string) {
	graphqlRejected.WithLabelValues(reason).Inc()
}
