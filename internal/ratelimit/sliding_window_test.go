package ratelimit

import (
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
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, mr
}

func TestSlidingWindow_AllowsUnderLimit(t *testing.T) {
	rdb, _ := newTestRedis(t)
	sw := NewSlidingWindow(rdb, 5, time.Minute)
	ctx := t.Context()

	for i := range 5 {
		allowed, remaining, _, err := sw.Allow(ctx, "client-1")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if !allowed {
			t.Fatalf("request %d: expected allowed, got denied", i)
		}
		if remaining != 5-i-1 {
			t.Errorf("request %d: remaining = %d, want %d", i, remaining, 5-i-1)
		}
	}
}

func TestSlidingWindow_BlocksAtLimit(t *testing.T) {
	rdb, _ := newTestRedis(t)
	sw := NewSlidingWindow(rdb, 3, time.Minute)
	ctx := t.Context()

	for range 3 {
		sw.Allow(ctx, "client-2") //nolint:errcheck
	}

	allowed, _, retryAfter, err := sw.Allow(ctx, "client-2")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if allowed {
		t.Fatal("expected denied at limit, got allowed")
	}
	if retryAfter <= 0 {
		t.Errorf("expected positive retryAfter, got %v", retryAfter)
	}
}

func TestSlidingWindow_ResetsAfterWindow(t *testing.T) {
	rdb, mr := newTestRedis(t)
	sw := NewSlidingWindow(rdb, 2, 10*time.Second)
	ctx := t.Context()

	// Fill up the limit
	sw.Allow(ctx, "client-3") //nolint:errcheck
	sw.Allow(ctx, "client-3") //nolint:errcheck

	allowed, _, _, _ := sw.Allow(ctx, "client-3")
	if allowed {
		t.Fatal("expected blocked at limit")
	}

	// Fast-forward time past the window in miniredis
	mr.FastForward(11 * time.Second)

	allowed, _, _, err := sw.Allow(ctx, "client-3")
	if err != nil {
		t.Fatalf("Allow after window: %v", err)
	}
	if !allowed {
		t.Fatal("expected allowed after window expired")
	}
}

func TestSlidingWindow_IndependentClients(t *testing.T) {
	rdb, _ := newTestRedis(t)
	sw := NewSlidingWindow(rdb, 1, time.Minute)
	ctx := t.Context()

	// client-a uses up its limit
	sw.Allow(ctx, "client-a") //nolint:errcheck
	allowed, _, _, _ := sw.Allow(ctx, "client-a")
	if allowed {
		t.Fatal("client-a should be blocked")
	}

	// client-b is unaffected
	allowed, _, _, err := sw.Allow(ctx, "client-b")
	if err != nil {
		t.Fatalf("client-b Allow: %v", err)
	}
	if !allowed {
		t.Fatal("client-b should be allowed (independent window)")
	}
}
