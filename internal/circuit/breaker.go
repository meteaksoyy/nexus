package circuit

import (
	"fmt"
	"time"

	"github.com/sony/gobreaker"
)

// Breakers holds one circuit breaker per named upstream service.
type Breakers struct {
	breakers map[string]*gobreaker.CircuitBreaker
}

// New creates a Breakers map for the given upstream names.
// Settings: open after 5 consecutive failures, attempt recovery after 30 seconds.
func New(upstreams ...string) *Breakers {
	b := &Breakers{breakers: make(map[string]*gobreaker.CircuitBreaker, len(upstreams))}
	for _, name := range upstreams {
		name := name // capture
		b.breakers[name] = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        name,
			MaxRequests: 1, // in half-open state, allow 1 probe request
			Interval:    0, // don't reset counts in closed state periodically
			Timeout:     30 * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures >= 5
			},
			OnStateChange: func(name string, from, to gobreaker.State) {
				// Logged by the upstream client; exposed as a Prometheus gauge in metrics.
				_ = fmt.Sprintf("circuit %s: %s → %s", name, from, to)
			},
		})
	}
	return b
}

// Execute runs fn through the named circuit breaker.
func (b *Breakers) Execute(upstream string, fn func() (any, error)) (any, error) {
	cb, ok := b.breakers[upstream]
	if !ok {
		return fn() // no breaker for this upstream — call directly
	}
	return cb.Execute(fn)
}

// State returns the current state of the named breaker as a string.
func (b *Breakers) State(upstream string) string {
	cb, ok := b.breakers[upstream]
	if !ok {
		return "unknown"
	}
	return cb.State().String()
}
