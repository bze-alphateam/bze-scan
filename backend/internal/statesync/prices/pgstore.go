package prices

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/jackc/pgx/v5/pgconn"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PGStore writes the set into the price columns of explorer.denoms.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// saveSQL prices every denom by the CoinGecko id of its registry asset: on
// BZE's own asset list ($1), else on its origin chain's for an IBC denom.
// The 24-hour change is not published by the aggregator and is left alone.
const saveSQL = `WITH priced AS (
		SELECT d.denom, p.price
		FROM explorer.denoms d
		LEFT JOIN explorer.registry_assets own ON own.chain_id = $1 AND own.base = d.denom
		LEFT JOIN explorer.registry_assets home ON home.chain_id = d.origin_chain_id AND home.base = d.ibc_base_denom
		LEFT JOIN unnest($2::text[], $3::numeric[]) AS p(id, price) ON p.id = coalesce(own.coingecko_id, home.coingecko_id)
	)
	UPDATE explorer.denoms d SET price_usd = priced.price, price_updated_at = now()
	FROM priced
	WHERE d.denom = priced.denom AND (priced.price IS NOT NULL OR d.price_usd IS NOT NULL)`

// Save writes the prices.
func (s *PGStore) Save(ctx context.Context, chainID string, prices map[string]string) error {
	ids := slices.Sorted(maps.Keys(prices))
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = prices[id]
	}
	if _, err := s.db.Exec(ctx, saveSQL, chainID, ids, values); err != nil {
		return fmt.Errorf("prices save: %w", err)
	}
	return nil
}
