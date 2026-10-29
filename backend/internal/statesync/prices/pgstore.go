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
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGStore writes the set into the price columns of explorer.denoms.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// saveSQL prices every denom by the DEX price chain ($4 denoms, $5 their
// prices) first, else by the CoinGecko id of its registry asset: on BZE's
// own asset list ($1), else on its origin chain's for an IBC denom. A denom
// the DEX pricer could not answer ($6) is left as it is.
const saveSQL = `WITH priced AS (
		SELECT d.denom, coalesce(x.price, p.price) AS price,
			CASE WHEN x.price IS NOT NULL THEN 'dex' WHEN p.price IS NOT NULL THEN 'coingecko' END AS source
		FROM explorer.denoms d
		LEFT JOIN unnest($4::text[], $5::numeric[]) AS x(denom, price) ON x.denom = d.denom
		LEFT JOIN explorer.registry_assets own ON own.chain_id = $1 AND own.base = d.denom
		LEFT JOIN explorer.registry_assets home ON home.chain_id = d.origin_chain_id AND home.base = d.ibc_base_denom
		LEFT JOIN unnest($2::text[], $3::numeric[]) AS p(id, price) ON p.id = coalesce(own.coingecko_id, home.coingecko_id)
		WHERE d.denom <> ALL($6::text[])
	)
	UPDATE explorer.denoms d SET price_usd = priced.price, price_source = priced.source, price_updated_at = now()
	FROM priced
	WHERE d.denom = priced.denom AND (priced.price IS NOT NULL OR d.price_usd IS NOT NULL)`

// changeSQL sets one denom's 24-hour change.
const changeSQL = `UPDATE explorer.denoms SET price_change_24h_pct = $2::numeric
	WHERE denom = $1 AND price_change_24h_pct IS DISTINCT FROM $2::numeric`

// Denoms lists every denom of explorer.denoms.
func (s *PGStore) Denoms(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT denom FROM explorer.denoms ORDER BY denom`)
	if err != nil {
		return nil, fmt.Errorf("prices denoms: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("prices denoms: %w", err)
	}
	return out, nil
}

// Save writes the prices and the change in one transaction.
func (s *PGStore) Save(ctx context.Context, snap Snapshot) error {
	ids := slices.Sorted(maps.Keys(snap.Prices))
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = snap.Prices[id]
	}
	dexDenoms := slices.Sorted(maps.Keys(snap.DEX))
	dexValues := make([]string, len(dexDenoms))
	for i, d := range dexDenoms {
		dexValues[i] = snap.DEX[d]
	}
	kept := snap.Kept
	if kept == nil {
		kept = []string{}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("prices save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	batch := &pgx.Batch{}
	batch.Queue(saveSQL, snap.ChainID, ids, values, dexDenoms, dexValues, kept)
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
