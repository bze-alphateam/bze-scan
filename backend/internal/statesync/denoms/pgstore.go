package denoms

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PGStore writes the set into explorer.denoms.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// created is the first token_events row of kind created of the denom $1,
// with its transaction's hash: the denom's first sight.
const created = `(SELECT %s FROM explorer.token_events e
		  LEFT JOIN explorer.transactions t ON t.height = e.height AND t.tx_index = e.tx_index
		  WHERE e.denom = $1 AND e.kind = 'created' ORDER BY e.height, e.tx_index, e.seq LIMIT 1)`

// upsertSQL writes one denom. The holders, price and IBC origin columns are
// other jobs' and left alone; first sight is the created token event when one
// is indexed, and a created event indexed later (by the backfill) fills it at
// the next resync.
var upsertSQL = `INSERT INTO explorer.denoms (
		denom, symbol, name, exponent, description, kind, creator, admin, logo_url,
		supply, halted, markets, metadata, created_height, created_tx_hash, created_time, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
		` + fmt.Sprintf(created, "e.height") + `, ` + fmt.Sprintf(created, "t.hash") + `, ` + fmt.Sprintf(created, "e.time") + `, now())
	ON CONFLICT (denom) DO UPDATE SET
		symbol = EXCLUDED.symbol, name = EXCLUDED.name, exponent = EXCLUDED.exponent,
		description = EXCLUDED.description, kind = EXCLUDED.kind, creator = EXCLUDED.creator,
		admin = EXCLUDED.admin, logo_url = EXCLUDED.logo_url, supply = EXCLUDED.supply,
		halted = EXCLUDED.halted, markets = EXCLUDED.markets, metadata = EXCLUDED.metadata,
		created_height = COALESCE(EXCLUDED.created_height, explorer.denoms.created_height),
		created_tx_hash = COALESCE(EXCLUDED.created_tx_hash, explorer.denoms.created_tx_hash),
		created_time = COALESCE(EXCLUDED.created_time, explorer.denoms.created_time),
		updated_at = now()`

// goneSQL zeroes the denoms a full resync did not list.
const goneSQL = `UPDATE explorer.denoms SET supply = 0, halted = false, markets = '{}', updated_at = now()
	WHERE denom <> ALL($1::text[]) AND (supply IS DISTINCT FROM 0 OR halted OR markets <> '{}')`

// Save writes a snapshot in one transaction.
func (s *PGStore) Save(ctx context.Context, snap Snapshot) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("denoms save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	listed := make([]string, 0, len(snap.Denoms))
	for _, d := range snap.Denoms {
		listed = append(listed, d.Denom)
		var metadata any
		if d.Metadata != nil {
			metadata = string(d.Metadata)
		}
		batch.Queue(upsertSQL, d.Denom, null(d.Symbol), null(d.Name), d.Exponent, null(d.Description), d.Kind,
			null(d.Creator), null(d.Admin), null(d.LogoURL), d.Supply, d.Halted, d.Markets, metadata)
	}
	if snap.Full {
		batch.Queue(goneSQL, listed)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("denoms save: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("denoms save: %w", err)
	}
	return nil
}

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}
