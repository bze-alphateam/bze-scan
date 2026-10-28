package dto

// The shapes of the aggregator-compatible routes (/api/…, outside /api/v1):
// field for field and type for type what the BZE aggregator API answers, so
// its callers can switch to the explorer unchanged.

// CompatPrice is one entry of GET /api/prices: a USD price by CoinGecko id,
// as a JSON number.
type CompatPrice struct {
	Denom      string  `json:"denom"`
	Price      float64 `json:"price"`
	PriceDenom string  `json:"price_denom"`
}
