package dto

import "github.com/bze-alphateam/bze-scan/backend/internal/orderbook"

// The shapes of the aggregator-compatible routes (/api/…, outside /api/v1):
// field for field and type for type what the BZE aggregator API answers, so
// its callers can switch to the explorer unchanged.

// CompatPrice is one entry of GET /aggregator/api/prices: a USD price by CoinGecko id,
// as a JSON number.
type CompatPrice struct {
	Denom      string  `json:"denom"`
	Price      float64 `json:"price"`
	PriceDenom string  `json:"price_denom"`
}

// The aggregator's error body and messages.
const (
	CompatInvalidRequest = "invalid request"
	CompatUnknownError   = "Unknown error! Please report this issue to the administrators at alphateam@getbze.com"
)

// CompatFormatCoinGecko is the format parameter's coingecko value; any other
// value is the default format.
const CompatFormatCoinGecko = "coingecko"

// CompatError is an aggregator error answer.
type CompatError struct {
	Message string `json:"message"`
}

// CompatLevel is one price level of GET /aggregator/api/dex/orders, display units as
// strings.
type CompatLevel struct {
	Price  string `json:"price"`
	Volume string `json:"volume"`
}

// CompatOrders is GET /aggregator/api/dex/orders' default format; timestamp is the
// answer's time in unix seconds, as a string.
type CompatOrders struct {
	MarketID  string        `json:"market_id"`
	Timestamp string        `json:"timestamp"`
	Bids      []CompatLevel `json:"bids"`
	Asks      []CompatLevel `json:"asks"`
}

// CompatCoinGeckoOrders is GET /aggregator/api/dex/orders?format=coingecko: levels as
// [price, volume] pairs under the ticker id <base>_<quote>.
type CompatCoinGeckoOrders struct {
	TickerID  string     `json:"ticker_id"`
	Timestamp string     `json:"timestamp"`
	Bids      [][]string `json:"bids"`
	Asks      [][]string `json:"asks"`
}

// NewCompatLevels converts levels; empty is [], never null.
func NewCompatLevels(levels []orderbook.Level) []CompatLevel {
	out := make([]CompatLevel, len(levels))
	for i, l := range levels {
		out[i] = CompatLevel{Price: l.Price, Volume: l.Volume}
	}
	return out
}

// NewCompatCoinGeckoLevels converts levels to pairs; empty is [], never
// null.
func NewCompatCoinGeckoLevels(levels []orderbook.Level) [][]string {
	out := make([][]string, len(levels))
	for i, l := range levels {
		out[i] = []string{l.Price, l.Volume}
	}
	return out
}
