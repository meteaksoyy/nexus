package resolvers

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/meteaksoyy/nexus/internal/ibkr"
)

// IBKRResolver handles GraphQL queries that fetch market data from the
// IBKR Client Portal Gateway.
type IBKRResolver struct {
	client *ibkr.Client
	log    zerolog.Logger
}

func NewIBKRResolver(client *ibkr.Client, log zerolog.Logger) *IBKRResolver {
	return &IBKRResolver{client: client, log: log}
}

// ── Query resolvers ──────────────────────────────────────────────────────────

func (r *IBKRResolver) Quote(ctx context.Context, args struct{ Symbol string }) (*MarketQuoteObject, error) {
	contract, err := r.client.SearchContract(ctx, args.Symbol)
	if err != nil {
		r.log.Warn().Err(err).Str("symbol", args.Symbol).Msg("ibkr contract search failed")
		return nil, fmt.Errorf("could not find contract for %q", args.Symbol)
	}

	quote, err := r.client.MarketSnapshot(ctx, contract.Conid)
	if err != nil {
		r.log.Warn().Err(err).Int("conid", contract.Conid).Msg("ibkr snapshot failed")
		return nil, fmt.Errorf("could not fetch quote for %q", args.Symbol)
	}
	quote.Currency = contract.Currency

	return &MarketQuoteObject{q: quote}, nil
}

func (r *IBKRResolver) MarketHistory(ctx context.Context, args struct {
	Symbol string
	Period *string
	Bar    *string
}) (*MarketHistoryObject, error) {
	contract, err := r.client.SearchContract(ctx, args.Symbol)
	if err != nil {
		return nil, fmt.Errorf("could not find contract for %q", args.Symbol)
	}

	period := ""
	if args.Period != nil {
		period = *args.Period
	}
	bar := ""
	if args.Bar != nil {
		bar = *args.Bar
	}

	history, err := r.client.MarketHistory(ctx, contract.Conid, period, bar)
	if err != nil {
		return nil, fmt.Errorf("could not fetch history for %q", args.Symbol)
	}
	if history.Symbol == "" {
		history.Symbol = args.Symbol
	}

	return &MarketHistoryObject{h: history}, nil
}

// ── Object resolvers ─────────────────────────────────────────────────────────

type MarketQuoteObject struct {
	q *ibkr.MarketQuote
}

func (o *MarketQuoteObject) Symbol() string    { return o.q.Symbol }
func (o *MarketQuoteObject) Last() float64     { return o.q.Last }
func (o *MarketQuoteObject) Bid() float64      { return o.q.Bid }
func (o *MarketQuoteObject) Ask() float64      { return o.q.Ask }
func (o *MarketQuoteObject) Change() float64   { return o.q.Change }
func (o *MarketQuoteObject) ChangePct() float64 { return o.q.ChangePct }
func (o *MarketQuoteObject) Volume() float64   { return o.q.Volume }
func (o *MarketQuoteObject) Currency() string  { return o.q.Currency }

type MarketHistoryObject struct {
	h *ibkr.HistoryResponse
}

func (o *MarketHistoryObject) Symbol() string { return o.h.Symbol }
func (o *MarketHistoryObject) Bars() []*OHLCVBarObject {
	out := make([]*OHLCVBarObject, len(o.h.Bars))
	for i := range o.h.Bars {
		out[i] = &OHLCVBarObject{b: &o.h.Bars[i]}
	}
	return out
}

type OHLCVBarObject struct {
	b *ibkr.HistoryBar
}

func (o *OHLCVBarObject) Time() string   { return o.b.Time }
func (o *OHLCVBarObject) Open() float64  { return o.b.Open }
func (o *OHLCVBarObject) High() float64  { return o.b.High }
func (o *OHLCVBarObject) Low() float64   { return o.b.Low }
func (o *OHLCVBarObject) Close() float64 { return o.b.Close }
func (o *OHLCVBarObject) Volume() float64 { return o.b.Volume }
