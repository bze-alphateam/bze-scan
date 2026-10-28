package sink

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PGStore is the Store over the sink tables in the public schema and the
// explorer's indexer_state.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// The blocks of a height range, by row id: every child table points at them.
const rangeBlocks = `SELECT rowid FROM public.blocks WHERE height >= $1 AND height < $2`

// Oldest returns min(height) of public.blocks.
func (s *PGStore) Oldest(ctx context.Context) (int64, bool, error) {
	return s.height(ctx, "oldest", `SELECT min(height) FROM public.blocks`)
}

// NewestBefore returns the highest height whose created_at is before t.
func (s *PGStore) NewestBefore(ctx context.Context, t time.Time) (int64, bool, error) {
	return s.height(ctx, "newest before", `SELECT max(height) FROM public.blocks WHERE created_at < $1`, t)
}

func (s *PGStore) height(ctx context.Context, what, sql string, args ...any) (int64, bool, error) {
	var h *int64
	if err := s.db.QueryRow(ctx, sql, args...).Scan(&h); err != nil {
		return 0, false, fmt.Errorf("sink %s: %w", what, err)
	}
	if h == nil {
		return 0, false, nil
	}
	return *h, true, nil
}

// LastIndexed returns indexer_state.last_indexed_height.
func (s *PGStore) LastIndexed(ctx context.Context) (int64, bool, error) {
	var v string
	err := s.db.QueryRow(ctx, `SELECT value FROM explorer.indexer_state WHERE key = $1`, writer.KeyLastIndexedHeight).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("sink: read the live cursor: %w", err)
	}
	h, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("sink: the live cursor %q is not a height", v)
	}
	return h, true, nil
}

// Count returns the rows of the heights in [from, to).
func (s *PGStore) Count(ctx context.Context, from, to int64) (Counts, error) {
	var n Counts
	err := s.db.QueryRow(ctx, `
		WITH b AS (`+rangeBlocks+`),
		     e AS (SELECT rowid FROM public.events WHERE block_id IN (SELECT rowid FROM b))
		SELECT
		  (SELECT count(*) FROM public.attributes WHERE event_id IN (SELECT rowid FROM e)),
		  (SELECT count(*) FROM e),
		  (SELECT count(*) FROM public.tx_results WHERE block_id IN (SELECT rowid FROM b)),
		  (SELECT count(*) FROM b)`, from, to).Scan(&n.Attributes, &n.Events, &n.TxResults, &n.Blocks)
	if err != nil {
		return Counts{}, fmt.Errorf("sink count %d..%d: %w", from, to-1, err)
	}
	return n, nil
}

// Delete deletes the rows of the heights in [from, to) in one transaction,
// children first, so it needs no ON DELETE CASCADE (and leaves a cascade
// nothing to do).
func (s *PGStore) Delete(ctx context.Context, from, to int64) (n Counts, err error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Counts{}, fmt.Errorf("sink delete: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	steps := []struct {
		dst *int64
		sql string
	}{
		{&n.Attributes, `DELETE FROM public.attributes WHERE event_id IN
			(SELECT rowid FROM public.events WHERE block_id IN (` + rangeBlocks + `))`},
		{&n.Events, `DELETE FROM public.events WHERE block_id IN (` + rangeBlocks + `)`},
		{&n.TxResults, `DELETE FROM public.tx_results WHERE block_id IN (` + rangeBlocks + `)`},
		{&n.Blocks, `DELETE FROM public.blocks WHERE height >= $1 AND height < $2`},
	}
	for _, st := range steps {
		tag, execErr := tx.Exec(ctx, st.sql, from, to)
		if execErr != nil {
			return Counts{}, fmt.Errorf("sink delete: %w", execErr)
		}
		*st.dst = tag.RowsAffected()
	}
	if err = tx.Commit(ctx); err != nil {
		return Counts{}, fmt.Errorf("sink delete: commit: %w", err)
	}
	return n, nil
}
