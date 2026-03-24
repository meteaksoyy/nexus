package cache

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()}), mr
}

func keyFn(r *http.Request) string {
	return "cache:" + r.URL.Path
}

func TestCacheMiddleware_Miss(t *testing.T) {
	rdb, _ := newTestRedis(t)
	m := NewMiddleware(rdb)

	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	m.Handler(time.Minute, keyFn)(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache: got %q, want MISS", rec.Header().Get("X-Cache"))
	}
	if calls.Load() != 1 {
		t.Errorf("upstream calls: got %d, want 1", calls.Load())
	}
}

func TestCacheMiddleware_Hit(t *testing.T) {
	rdb, _ := newTestRedis(t)
	m := NewMiddleware(rdb)

	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"ok":true}`))
	})

	mw := m.Handler(time.Minute, keyFn)(handler)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)

	// First request — miss
	mw.ServeHTTP(httptest.NewRecorder(), req)

	// Second request — should hit cache
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("X-Cache: got %q, want HIT", rec.Header().Get("X-Cache"))
	}
	if calls.Load() != 1 {
		t.Errorf("upstream calls: got %d, want 1 (second should be cached)", calls.Load())
	}
}

func TestCacheMiddleware_SkipsNonGET(t *testing.T) {
	rdb, _ := newTestRedis(t)
	m := NewMiddleware(rdb)

	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"ok":true}`))
	})

	mw := m.Handler(time.Minute, keyFn)(handler)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/test", nil)
		mw.ServeHTTP(httptest.NewRecorder(), req)
	}

	if calls.Load() != 3 {
		t.Errorf("upstream calls: got %d, want 3 (non-GET bypasses cache)", calls.Load())
	}
}

func TestCacheMiddleware_SkipsNoCacheHeader(t *testing.T) {
	rdb, _ := newTestRedis(t)
	m := NewMiddleware(rdb)

	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"ok":true}`))
	})

	mw := m.Handler(time.Minute, keyFn)(handler)

	// First request populates cache
	mw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/test", nil))

	// Second request with Cache-Control: no-cache bypasses it
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Cache-Control", "no-cache")
	mw.ServeHTTP(httptest.NewRecorder(), req)

	if calls.Load() != 2 {
		t.Errorf("upstream calls: got %d, want 2 (no-cache should bypass)", calls.Load())
	}
}

func TestCacheMiddleware_Singleflight(t *testing.T) {
	rdb, _ := newTestRedis(t)
	m := NewMiddleware(rdb)

	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond) // simulate slow upstream
		w.Write([]byte(`{"ok":true}`))
	})

	mw := m.Handler(time.Minute, keyFn)(handler)

	// Fire 10 concurrent requests — all miss cache simultaneously
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/sf", nil)
			mw.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	wg.Wait()

	// Singleflight should have collapsed all into 1 upstream call
	if calls.Load() != 1 {
		t.Errorf("upstream calls: got %d, want 1 (singleflight should deduplicate)", calls.Load())
	}
}

func TestCacheMiddleware_TTLExpiry(t *testing.T) {
	rdb, mr := newTestRedis(t)
	m := NewMiddleware(rdb)

	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"ok":true}`))
	})

	mw := m.Handler(5*time.Second, keyFn)(handler)
	req := httptest.NewRequest(http.MethodGet, "/api/ttl", nil)

	// Populate cache
	mw.ServeHTTP(httptest.NewRecorder(), req)

	// Advance time past TTL
	mr.FastForward(6 * time.Second)

	// Should miss and call upstream again
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if calls.Load() != 2 {
		t.Errorf("upstream calls: got %d, want 2 (cache should have expired)", calls.Load())
	}
	if rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache: got %q, want MISS after TTL", rec.Header().Get("X-Cache"))
	}
}
