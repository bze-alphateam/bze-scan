package ordercoverage

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PGStore reads the indexed range from the explorer tables.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// LastIndexed reads indexer_state.last_indexed_height.
func (s *PGStore) LastIndexed(ctx context.Context) (int64, bool, error) {
	var raw string
	err := s.db.QueryRow(ctx,
		`SELECT value FROM explorer.indexer_state WHERE key = 'last_indexed_height'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("order coverage cursor: %w", err)
	}
	h, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("order coverage cursor %q: %w", raw, err)
	}
	return h, true, nil
}

// CountBlocks counts the blocks rows in [lo, hi] (an index-only scan of the
// primary key).
func (s *PGStore) CountBlocks(ctx context.Context, lo, hi int64) (int64, error) {
	var n int64
	if err := s.db.QueryRow(ctx,
		`SELECT count(*) FROM explorer.blocks WHERE height BETWEEN $1 AND $2`, lo, hi).Scan(&n); err != nil {
		return 0, fmt.Errorf("order coverage blocks: %w", err)
	}
	return n, nil
}

// OpenFailures reports whether an unresolved index failure lies in [lo, hi].
func (s *PGStore) OpenFailures(ctx context.Context, lo, hi int64) (bool, error) {
	var found bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM explorer.index_failures
		WHERE resolved_at IS NULL AND height BETWEEN $1 AND $2)`, lo, hi).Scan(&found); err != nil {
		return false, fmt.Errorf("order coverage failures: %w", err)
	}
	return found, nil
}
