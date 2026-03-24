package ibkr

// AuthStatus is decoded from GET /v1/api/iserver/auth/status.
type AuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	Competing     bool   `json:"competing"`
	Connected     bool   `json:"connected"`
	Message       string `json:"message"`
}

// ContractInfo is one result from GET /v1/api/iserver/search/contract.
type ContractInfo struct {
	Conid    int    `json:"conid"`
	Symbol   string `json:"symbol"`
	Name     string `json:"companyName"`
	Exchange string `json:"primaryExch"`
	Currency string `json:"currency"`
	SecType  string `json:"secType"`
}

// MarketQuote is the normalised snapshot for a single instrument.
// Raw IBKR snapshot fields: 31=last, 55=symbol, 84=bid, 86=ask,
// 88=volume, 7295=changePct, 7296=change.
type MarketQuote struct {
	Conid     int
	Symbol    string
	Last      float64
	Bid       float64
	Ask       float64
	Change    float64
	ChangePct float64
	Volume    float64
	Currency  string
}

// HistoryBar is one OHLCV bar from GET /v1/api/iserver/history/data.
type HistoryBar struct {
	Time   string  `json:"t"`
	Open   float64 `json:"o"`
	High   float64 `json:"h"`
	Low    float64 `json:"l"`
	Close  float64 `json:"c"`
	Volume float64 `json:"v"`
}

// HistoryResponse is the full response from the history endpoint.
type HistoryResponse struct {
	Symbol string       `json:"symbol"`
	Bars   []HistoryBar `json:"data"`
}

// TickleResponse is returned by POST /v1/api/tickle.
type TickleResponse struct {
	Session string `json:"session"`
	HMDS    struct {
		Error string `json:"error"`
	} `json:"hmds"`
}
