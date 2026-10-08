package status

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Querier runs a single-row query; *pgxpool.Pool implements it.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PGStore is the Store over the explorer schema.
type PGStore struct {
	db Querier
}

// NewPGStore returns a Store reading through db.
func NewPGStore(db Querier) *PGStore {
	return &PGStore{db: db}
}

// LastIndexedHeight implements Store.
func (s *PGStore) LastIndexedHeight(ctx context.Context) (int64, bool, error) {
	var h int64
	err := s.db.QueryRow(ctx,
		`SELECT value::bigint FROM explorer.indexer_state WHERE key = 'last_indexed_height'`).Scan(&h)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return h, err == nil, err
}

// OldestHeight implements Store.
func (s *PGStore) OldestHeight(ctx context.Context) (int64, bool, error) {
	var h *int64
	if err := s.db.QueryRow(ctx, `SELECT min(height) FROM explorer.blocks`).Scan(&h); err != nil {
		return 0, false, err
	}
	if h == nil {
		return 0, false, nil
	}
	return *h, true, nil
}

// BackfillStatus implements Store.
func (s *PGStore) BackfillStatus(ctx context.Context) (string, bool, error) {
	var st string
	err := s.db.QueryRow(ctx,
		`SELECT status FROM explorer.backfill_checkpoints WHERE job = 'main'`).Scan(&st)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return st, err == nil, err
}
