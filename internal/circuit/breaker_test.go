package circuit

import (
	"errors"
	"testing"
)

var errUpstream = errors.New("upstream failure")

func TestBreaker_ClosedAllowsRequests(t *testing.T) {
	b := New("svc-a")

	result, err := b.Execute("svc-a", func() (any, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "ok" {
		t.Errorf("result: got %v, want ok", result)
	}
	if b.State("svc-a") != "closed" {
		t.Errorf("state: got %s, want closed", b.State("svc-a"))
	}
}

func TestBreaker_OpensAfterConsecutiveFailures(t *testing.T) {
	b := New("svc-b")

	for range 5 {
		b.Execute("svc-b", func() (any, error) { //nolint:errcheck
			return nil, errUpstream
		})
	}

	if b.State("svc-b") != "open" {
		t.Errorf("state: got %s, want open after 5 failures", b.State("svc-b"))
	}
}

func TestBreaker_OpenRejectsImmediately(t *testing.T) {
	b := New("svc-c")

	for range 5 {
		b.Execute("svc-c", func() (any, error) { //nolint:errcheck
			return nil, errUpstream
		})
	}

	var upstreamCalled bool
	_, err := b.Execute("svc-c", func() (any, error) {
		upstreamCalled = true
		return "ok", nil
	})

	if err == nil {
		t.Fatal("expected error from open circuit, got nil")
	}
	if upstreamCalled {
		t.Fatal("upstream should not be called when circuit is open")
	}
}

func TestBreaker_UnknownUpstreamCallsDirectly(t *testing.T) {
	b := New("svc-d")

	called := false
	result, err := b.Execute("unknown-svc", func() (any, error) {
		called = true
		return "direct", nil
	})

	if err != nil {
		t.Fatalf("Execute unknown: %v", err)
	}
	if !called {
		t.Fatal("fn should be called directly when no breaker registered")
	}
	if result != "direct" {
		t.Errorf("result: got %v, want direct", result)
	}
}

func TestBreaker_ResetOnSuccess(t *testing.T) {
	b := New("svc-e")

	// 4 failures — not enough to open
	for range 4 {
		b.Execute("svc-e", func() (any, error) { //nolint:errcheck
			return nil, errUpstream
		})
	}

	// One success resets consecutive failure count
	b.Execute("svc-e", func() (any, error) { //nolint:errcheck
		return "ok", nil
	})

	if b.State("svc-e") != "closed" {
		t.Errorf("state: got %s, want closed after success", b.State("svc-e"))
	}

	// 4 more failures still should not open (consecutive count was reset)
	for range 4 {
		b.Execute("svc-e", func() (any, error) { //nolint:errcheck
			return nil, errUpstream
		})
	}
	if b.State("svc-e") != "closed" {
		t.Errorf("state: got %s, want still closed (only 4 consecutive)", b.State("svc-e"))
	}
}

func TestBreaker_StateUnknownUpstream(t *testing.T) {
	b := New("svc-f")
	if b.State("nonexistent") != "unknown" {
		t.Errorf("State of unregistered upstream: got %s, want unknown", b.State("nonexistent"))
	}
}

func TestBreaker_FourFailuresDoesNotOpen(t *testing.T) {
	b := New("svc-g")

	for range 4 {
		b.Execute("svc-g", func() (any, error) { //nolint:errcheck
			return nil, errUpstream
		})
	}

	if b.State("svc-g") != "closed" {
		t.Errorf("state: got %s, want closed (threshold is 5)", b.State("svc-g"))
	}

	// 5th failure tips it over
	b.Execute("svc-g", func() (any, error) { //nolint:errcheck
		return nil, errUpstream
	})
	if b.State("svc-g") != "open" {
		t.Errorf("state: got %s, want open at exactly 5 failures", b.State("svc-g"))
	}
}
