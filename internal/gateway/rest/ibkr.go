package rest

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
	"github.com/meteaksoyy/nexus/internal/ibkr"
)

// IBKRHandlers exposes market data endpoints backed by a local IBKR Client Portal Gateway.
type IBKRHandlers struct {
	client *ibkr.Client
	log    zerolog.Logger
}

func NewIBKRHandlers(client *ibkr.Client, log zerolog.Logger) *IBKRHandlers {
	return &IBKRHandlers{client: client, log: log.With().Str("handler", "ibkr").Logger()}
}

// Quote handles GET /api/v1/market/quote?symbol=AAPL
// Looks up the contract by symbol then fetches a live/delayed snapshot.
func (h *IBKRHandlers) Quote(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, `{"error":"symbol is required"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	contract, err := h.client.SearchContract(ctx, symbol)
	if err != nil {
		h.log.Warn().Err(err).Str("symbol", symbol).Msg("contract search failed")
		h.writeUpstreamError(w, err)
		return
	}

	quote, err := h.client.MarketSnapshot(ctx, contract.Conid)
	if err != nil {
		h.log.Warn().Err(err).Int("conid", contract.Conid).Msg("market snapshot failed")
		h.writeUpstreamError(w, err)
		return
	}
	quote.Currency = contract.Currency

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(quote)
}

// History handles GET /api/v1/market/history?symbol=AAPL&period=1d&bar=1h
func (h *IBKRHandlers) History(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, `{"error":"symbol is required"}`, http.StatusBadRequest)
		return
	}
	period := r.URL.Query().Get("period")
	bar := r.URL.Query().Get("bar")

	ctx := r.Context()

	contract, err := h.client.SearchContract(ctx, symbol)
	if err != nil {
		h.log.Warn().Err(err).Str("symbol", symbol).Msg("contract search failed")
		h.writeUpstreamError(w, err)
		return
	}

	history, err := h.client.MarketHistory(ctx, contract.Conid, period, bar)
	if err != nil {
		h.log.Warn().Err(err).Int("conid", contract.Conid).Msg("market history failed")
		h.writeUpstreamError(w, err)
		return
	}
	if history.Symbol == "" {
		history.Symbol = symbol
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

// Search handles GET /api/v1/market/search?symbol=AAPL
// Returns contract metadata including the conid — useful for callers who need
// to make subsequent quote or history calls with a known conid.
func (h *IBKRHandlers) Search(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, `{"error":"symbol is required"}`, http.StatusBadRequest)
		return
	}

	contract, err := h.client.SearchContract(r.Context(), symbol)
	if err != nil {
		h.log.Warn().Err(err).Str("symbol", symbol).Msg("contract search failed")
		h.writeUpstreamError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contract)
}

func (h *IBKRHandlers) writeUpstreamError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case msg == "ibkr session not authenticated":
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"ibkr session not authenticated — log in at https://localhost:5000"}`, http.StatusServiceUnavailable)
	case contains(msg, "no contract found"):
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"`+msg+`"}`, http.StatusNotFound)
	default:
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"upstream error"}`, http.StatusBadGateway)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
