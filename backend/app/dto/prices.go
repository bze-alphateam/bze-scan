package dto

import (
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// Prices is GET /api/v1/prices: every priced denom, by denom.
type Prices struct {
	Prices []Price `json:"prices"`
}

// Price is one priced denom: the USD price and the 24-hour change as decimal
// strings like the token DTOs', the source (dex: the DEX price chain,
// coingecko: by chain-registry id) and when the prices job last wrote it.
type Price struct {
	Denom             string     `json:"denom"`
	PriceUSD          string     `json:"price_usd"`
	PriceSource       *string    `json:"price_source"`
	PriceChange24hPct *string    `json:"price_change_24h_pct"`
	UpdatedAt         *time.Time `json:"updated_at"`
}

// NewPrices maps the priced denoms rows.
func NewPrices(rows []repository.Price) Prices {
	out := Prices{Prices: make([]Price, 0, len(rows))}
	for _, r := range rows {
		out.Prices = append(out.Prices, Price{Denom: r.Denom, PriceUSD: r.PriceUSD, PriceSource: r.PriceSource,
			PriceChange24hPct: r.PriceChange24hPct, UpdatedAt: utc(r.UpdatedAt)})
	}
	return out
}
