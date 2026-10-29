package dto

import (
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
)

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

// CompatHistoryTrade is one trade of GET /aggregator/api/dex/swaps:
// display units as strings, the id a JSON number, executed_at the trade's
// time in unix milliseconds as a string. A swap names no maker ("") and
// its pool id and denoms; pool_id, base and quote are left out when empty.
type CompatHistoryTrade struct {
	OrderID     int64  `json:"order_id"`
	Price       string `json:"price"`
	BaseVolume  string `json:"base_volume"`
	QuoteVolume string `json:"quote_volume"`
	ExecutedAt  string `json:"executed_at"`
	OrderType   string `json:"order_type"`
	Maker       string `json:"maker"`
	Taker       string `json:"taker"`
	PoolID      string `json:"pool_id,omitempty"`
	Base        string `json:"base,omitempty"`
	Quote       string `json:"quote,omitempty"`
}

// CompatInterval is one candle of GET /aggregator/api/dex/intervals' default
// format, display units as strings.
type CompatInterval struct {
	MarketID     string    `json:"market_id"`
	Minutes      int       `json:"minutes"`
	StartAt      time.Time `json:"start_at"`
	EndAt        time.Time `json:"end_at"`
	LowestPrice  string    `json:"lowest_price"`
	OpenPrice    string    `json:"open_price"`
	AveragePrice string    `json:"average_price"`
	HighestPrice string    `json:"highest_price"`
	ClosePrice   string    `json:"close_price"`
	BaseVolume   string    `json:"base_volume"`
	QuoteVolume  string    `json:"quote_volume"`
}

// CompatTVInterval is one candle of GET
// /aggregator/api/dex/intervals?format=tv (TradingView): the start in unix
// seconds, the prices and the base volume as numbers.
type CompatTVInterval struct {
	Time  int64   `json:"time"`
	Low   float64 `json:"low"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Close float64 `json:"close"`
	Value float64 `json:"value"`
}
