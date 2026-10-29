package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Price is a priced denoms row: its USD price, where it comes from (dex,
// coingecko), its 24-hour change (the native denom's only) and when the
// prices job last wrote it.
type Price struct {
	Denom             string
	PriceUSD          string
	PriceSource       *string
	PriceChange24hPct *string
	UpdatedAt         *time.Time
}

// Prices returns every denom with a price, by denom.
func (r *Explorer) Prices(ctx context.Context) ([]Price, error) {
	rows, err := r.db.Query(ctx, `SELECT denom, price_usd::text, price_source, price_change_24h_pct::text, price_updated_at
		FROM explorer.denoms WHERE price_usd IS NOT NULL ORDER BY denom`)
	if err != nil {
		return nil, fmt.Errorf("prices: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Price, error) {
		var p Price
		err := row.Scan(&p.Denom, &p.PriceUSD, &p.PriceSource, &p.PriceChange24hPct, &p.UpdatedAt)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("prices: %w", err)
	}
	return out, nil
}
