//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/meteaksoyy/nexus/config"
	"github.com/meteaksoyy/nexus/internal/gateway"
	"github.com/rs/zerolog"
	"net/http/httptest"
)

// newTestEnvWithLimits creates an env with a custom tight rate limit for testing.
func newTestEnvWithLimits(t *testing.T, limit int) *testEnv {
	t.Helper()
	env := newTestEnv(t)

	// Rebuild the server with a tighter rate limit
	cfg := *env.cfg
	cfg.RateLimitAuthed = limit
	cfg.RateLimitAPIKey = limit
	cfg.RateLimitWindow = time.Minute

	log := zerolog.Nop()
	router := gateway.NewRouter(&cfg, env.pool, env.rdb, nil, log)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	env.server = server
	env.cfg = &cfg
	return env
}

func TestRateLimit_BlocksAtLimit(t *testing.T) {
	env := newTestEnvWithLimits(t, 3)
	token, _ := registerAndLogin(t, env, "rl@example.com", "pass123")

	client := &http.Client{}
	makeRequest := func() int {
		req, _ := http.NewRequest(http.MethodGet, env.url("/api/v1/github/users/octocat"), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// First 3 should pass (or 200/502 — upstream may fail in test, that's fine)
	for i := range 3 {
		status := makeRequest()
		if status == http.StatusTooManyRequests {
			t.Fatalf("request %d: got 429 before limit was reached", i+1)
		}
	}

	// 4th request must be rate limited
	if status := makeRequest(); status != http.StatusTooManyRequests {
		t.Errorf("4th request: got %d, want 429", status)
	}
}

func TestRateLimit_RetryAfterHeader(t *testing.T) {
	env := newTestEnvWithLimits(t, 1)
	token, _ := registerAndLogin(t, env, "retry@example.com", "pass123")

	client := &http.Client{}
	makeReq := func() *http.Response {
		req, _ := http.NewRequest(http.MethodGet, env.url("/api/v1/github/users/octocat"), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, _ := client.Do(req)
		return resp
	}

	makeReq().Body.Close() // consume the 1 allowed request

	resp := makeReq()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}

	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header on 429 response")
	}
	secs, err := strconv.Atoi(retryAfter)
	if err != nil || secs <= 0 {
		t.Errorf("Retry-After should be a positive integer, got %q", retryAfter)
	}
}

func TestRateLimit_AuthRouteNotLimited(t *testing.T) {
	env := newTestEnvWithLimits(t, 2)

	// Auth routes should not be rate limited regardless of the limit
	for i := range 5 {
		body, _ := json.Marshal(map[string]string{
			"email":    "notlimited@example.com",
			"password": "pass",
		})
		resp, _ := http.Post(env.url("/auth/register"), "application/json", bytes.NewReader(body))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Errorf("request %d: /auth/register should not be rate limited", i+1)
		}
	}
}

func TestRateLimit_HealthNotLimited(t *testing.T) {
	env := newTestEnvWithLimits(t, 1)

	for range 10 {
		resp, _ := http.Get(env.url("/health"))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatal("/health should never be rate limited")
		}
	}
}
