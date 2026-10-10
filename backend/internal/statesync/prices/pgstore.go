package prices

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
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

// changeSQL sets one denom's 24-hour change.
const changeSQL = `UPDATE explorer.denoms SET price_change_24h_pct = $2::numeric
	WHERE denom = $1 AND price_change_24h_pct IS DISTINCT FROM $2::numeric`

// Save writes the prices and the change in one transaction.
func (s *PGStore) Save(ctx context.Context, snap Snapshot) error {
	ids := slices.Sorted(maps.Keys(snap.Prices))
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = snap.Prices[id]
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("prices save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	batch := &pgx.Batch{}
	batch.Queue(saveSQL, snap.ChainID, ids, values)
	if !snap.KeepChange && snap.ChangeDenom != "" {
		batch.Queue(changeSQL, snap.ChangeDenom, snap.Change)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("prices save: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("prices save: %w", err)
	}
	return nil
}
