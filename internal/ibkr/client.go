package ibkr

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Client communicates with a locally running IBKR Client Portal Gateway.
// It manages session cookies, keeps the session alive via periodic tickles,
// and optionally authenticates on startup if credentials are provided.
type Client struct {
	http     *http.Client
	baseURL  string
	username string
	password string
	session  *SessionManager
	log      zerolog.Logger
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// New creates an IBKR client. The http.Client uses a cookie jar (for session
// cookies) and skips TLS verification — scoped to this client only, the local
// gateway uses a self-signed certificate.
//
// If username and password are non-empty, Start will attempt auto-authentication.
// Otherwise the user must authenticate manually via the browser.
func New(baseURL, username, password string, log zerolog.Logger) *Client {
	jar, _ := cookiejar.New(nil)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local gateway uses self-signed cert
	}
	return &Client{
		http: &http.Client{
			Jar:       jar,
			Transport: transport,
			Timeout:   10 * time.Second,
		},
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		session:  newSessionManager(),
		log:      log.With().Str("component", "ibkr").Logger(),
		stopCh:   make(chan struct{}),
	}
}

// Start checks the current auth status, optionally authenticates, and launches
// the keepalive goroutine. It returns immediately — call Stop to shut down.
func (c *Client) Start(ctx context.Context) error {
	status, err := c.AuthStatus(ctx)
	if err != nil {
		c.session.Set(StateUnauthenticated)
		c.log.Warn().Err(err).Msg("ibkr gateway unreachable on startup; will retry via keepalive")
	} else if status.Authenticated {
		c.session.Set(StateAuthenticated)
		c.log.Info().Msg("ibkr session already authenticated")
	} else {
		c.session.Set(StateUnauthenticated)
		if c.username != "" && c.password != "" {
			if authErr := c.Authenticate(ctx, c.username, c.password); authErr != nil {
				c.log.Warn().Err(authErr).Msg("ibkr auto-auth failed; authenticate manually at https://localhost:5000")
			}
		} else {
			c.log.Warn().Msg("ibkr session not authenticated; set IBKR_USERNAME/IBKR_PASSWORD or log in via browser")
		}
	}

	c.wg.Add(1)
	go c.keepalive()
	return nil
}

// Stop signals the keepalive goroutine to exit and waits for it to finish.
func (c *Client) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

// keepalive sends a tickle every 55 seconds to maintain the session.
func (c *Client) keepalive() {
	defer c.wg.Done()
	ticker := time.NewTicker(55 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := c.Tickle(ctx); err != nil {
				c.log.Warn().Err(err).Msg("ibkr tickle failed")
				c.session.Set(StateUnauthenticated)
			} else {
				c.session.Set(StateAuthenticated)
			}
			cancel()
		}
	}
}

// AuthStatus calls GET /v1/api/iserver/auth/status.
func (c *Client) AuthStatus(ctx context.Context) (*AuthStatus, error) {
	var out AuthStatus
	if err := c.get(ctx, "/v1/api/iserver/auth/status", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Authenticate attempts username/password login via the gateway's OIDC init endpoint.
// On success the session cookie is stored in the cookie jar automatically.
func (c *Client) Authenticate(ctx context.Context, username, password string) error {
	payload := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/api/iserver/auth/ssodh/init", strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ibkr auth failed (%d): %s", resp.StatusCode, body)
	}
	c.session.Set(StateAuthenticated)
	c.log.Info().Msg("ibkr auto-authentication succeeded")
	return nil
}

// Tickle calls POST /v1/api/tickle to keep the session alive.
func (c *Client) Tickle(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/api/tickle", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("tickle returned %d", resp.StatusCode)
	}
	return nil
}

// SearchContract calls GET /v1/api/iserver/search/contract and returns the
// first STK result. Falls back to the first result of any type if no STK found.
func (c *Client) SearchContract(ctx context.Context, symbol string) (*ContractInfo, error) {
	var results []ContractInfo
	if err := c.get(ctx, "/v1/api/iserver/search/contract?symbol="+symbol, &results); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no contract found for symbol %q", symbol)
	}
	for i := range results {
		if results[i].SecType == "STK" {
			return &results[i], nil
		}
	}
	return &results[0], nil
}

// MarketSnapshot retrieves a live/delayed quote for a conid.
// IBKR requires a subscription call (first request) before data is available,
// so this method retries up to 3 times with a 300 ms pause.
func (c *Client) MarketSnapshot(ctx context.Context, conid int) (*MarketQuote, error) {
	path := fmt.Sprintf("/v1/api/iserver/marketdata/snapshot?conids=%d&fields=31,55,84,86,88,7295,7296", conid)

	var raw []map[string]any
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
		if err := c.get(ctx, path, &raw); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if _, ok := raw[0]["31"]; ok {
				break
			}
		}
		raw = nil
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("snapshot returned no data for conid %d", conid)
	}
	return parseSnapshot(raw[0], conid), nil
}

// parseSnapshot converts the stringly-typed IBKR snapshot map to a MarketQuote.
// Missing or unparseable fields are returned as zero values rather than errors.
func parseSnapshot(m map[string]any, conid int) *MarketQuote {
	q := &MarketQuote{Conid: conid}
	q.Symbol = fieldStr(m, "55")
	q.Last = fieldFloat(m, "31")
	q.Bid = fieldFloat(m, "84")
	q.Ask = fieldFloat(m, "86")
	q.Volume = fieldFloat(m, "88")
	q.ChangePct = fieldFloat(m, "7295")
	q.Change = fieldFloat(m, "7296")
	return q
}

func fieldStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func fieldFloat(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case string:
		if t == "--" || t == "" {
			return 0
		}
		f, _ := strconv.ParseFloat(t, 64)
		return f
	case json.Number:
		f, _ := t.Float64()
		return f
	}
	return 0
}

// MarketHistory retrieves OHLCV bars for a conid.
// period examples: "1d", "1w", "1m". bar examples: "1h", "1d".
func (c *Client) MarketHistory(ctx context.Context, conid int, period, bar string) (*HistoryResponse, error) {
	if period == "" {
		period = "1d"
	}
	if bar == "" {
		bar = "1h"
	}
	path := fmt.Sprintf("/v1/api/iserver/history/data?conid=%d&period=%s&bar=%s&outsideRth=false",
		conid, period, bar)
	var out HistoryResponse
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// get is a convenience wrapper for JSON GET requests to the gateway.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		c.session.Set(StateUnauthenticated)
		return fmt.Errorf("ibkr session not authenticated")
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ibkr %s returned %d: %s", path, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
