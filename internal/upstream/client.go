package upstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/user/nexus/internal/circuit"
)

const (
	defaultTimeout    = 5 * time.Second
	defaultMaxRetries = 3
	defaultBackoff    = 100 * time.Millisecond
)

// Client is a resilient HTTP client that wraps retries, per-request timeouts,
// and a circuit breaker for a named upstream service.
type Client struct {
	http     *http.Client
	breakers *circuit.Breakers
	name     string
	maxRetry int
	backoff  time.Duration
}

// New returns a Client for the named upstream.
func New(name string, breakers *circuit.Breakers, opts ...Option) *Client {
	c := &Client{
		http:     &http.Client{Timeout: defaultTimeout},
		breakers: breakers,
		name:     name,
		maxRetry: defaultMaxRetries,
		backoff:  defaultBackoff,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Option configures a Client.
type Option func(*Client)

func WithTimeout(d time.Duration) Option { return func(c *Client) { c.http.Timeout = d } }
func WithMaxRetries(n int) Option        { return func(c *Client) { c.maxRetry = n } }

// Get performs a GET request through the circuit breaker with retry + backoff.
// Retries on 5xx responses and transient network errors only.
func (c *Client) Get(ctx context.Context, url string, headers map[string]string) ([]byte, int, error) {
	var (
		body       []byte
		statusCode int
	)

	_, err := c.breakers.Execute(c.name, func() (any, error) {
		var lastErr error
		for attempt := 0; attempt < c.maxRetry; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(c.backoff * time.Duration(1<<(attempt-1))): // exponential backoff
				}
			}

			b, code, err := c.do(ctx, url, headers)
			if err != nil {
				lastErr = err
				continue // retry on network error
			}
			if code >= 500 {
				lastErr = fmt.Errorf("upstream %s returned %d", c.name, code)
				continue // retry on server error
			}

			body, statusCode = b, code
			return nil, nil
		}
		return nil, fmt.Errorf("upstream %s: %w", c.name, lastErr)
	})

	return body, statusCode, err
}

func (c *Client) do(ctx context.Context, url string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	return b, resp.StatusCode, nil
}
