package chainstate

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PGStore reads the validators and denoms tables and writes
// explorer.chain_state.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// ValidatorCounts counts the validators rows.
func (s *PGStore) ValidatorCounts(ctx context.Context) (ValidatorCounts, error) {
	var c ValidatorCounts
	err := s.db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'bonded'), count(*) FILTER (WHERE jailed), count(*)
		FROM explorer.validators`).Scan(&c.Bonded, &c.Jailed, &c.Total)
	if err != nil {
		return c, fmt.Errorf("validator counts: %w", err)
	}
	return c, nil
}

// Price reads a denom's price columns; both null for an unknown denom.
func (s *PGStore) Price(ctx context.Context, denom string) (Price, error) {
	var p Price
	err := s.db.QueryRow(ctx, `SELECT price_usd::text, price_change_24h_pct::text FROM explorer.denoms WHERE denom = $1`,
		denom).Scan(&p.PriceUSD, &p.PriceChange24hPct)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return p, fmt.Errorf("price of %s: %w", denom, err)
	}
	return p, nil
}

// Save upserts the rows in one transaction.
func (s *PGStore) Save(ctx context.Context, rows []Row) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("chain state save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue(`INSERT INTO explorer.chain_state (key, value, height, updated_at) VALUES ($1, $2, $3, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, height = EXCLUDED.height, updated_at = EXCLUDED.updated_at`,
			r.Key, string(r.Value), r.Height)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("chain state save: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("chain state save: %w", err)
	}
	return nil
}
