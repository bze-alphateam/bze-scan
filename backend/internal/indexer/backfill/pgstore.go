package backfill

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB is the part of a PostgreSQL pool the store uses; *pgxpool.Pool
// satisfies it.
type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PGStore is the Store over the explorer schema.
type PGStore struct {
	db DB
}

// NewPGStore returns a store reading and writing through db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// LiveFloor implements Store.
func (s *PGStore) LiveFloor(ctx context.Context) (int64, bool, error) {
	var v string
	err := s.db.QueryRow(ctx, `SELECT value FROM explorer.indexer_state WHERE key = 'live_floor'`).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read live_floor: %w", err)
	}
	h, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("read live_floor: %q is not a height", v)
	}
	return h, true, nil
}

// PresentHeights implements Presence.
func (s *PGStore) PresentHeights(ctx context.Context, lo, hi int64) ([]int64, error) {
	rows, err := s.db.Query(ctx, `SELECT height FROM explorer.blocks WHERE height BETWEEN $1 AND $2`, lo, hi)
	if err != nil {
		return nil, fmt.Errorf("present heights %d..%d: %w", lo, hi, err)
	}
	heights, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("present heights %d..%d: %w", lo, hi, err)
	}
	return heights, nil
}

// Checkpoint implements Store.
func (s *PGStore) Checkpoint(ctx context.Context, job string) (Checkpoint, bool, error) {
	cp := Checkpoint{Job: job}
	var lastError *string
	err := s.db.QueryRow(ctx, `SELECT ceiling_height, floor_height, lowest_dispatched, blocks_done, status, last_error
		FROM explorer.backfill_checkpoints WHERE job = $1`, job).
		Scan(&cp.Ceiling, &cp.Floor, &cp.LowestDispatched, &cp.BlocksDone, &cp.Status, &lastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return Checkpoint{}, false, nil
	}
	if err != nil {
		return Checkpoint{}, false, fmt.Errorf("read checkpoint %s: %w", job, err)
	}
	if lastError != nil {
		cp.LastError = *lastError
	}
	return cp, true, nil
}

// SaveCheckpoint implements Store.
func (s *PGStore) SaveCheckpoint(ctx context.Context, cp Checkpoint) error {
	var lastError *string
	if cp.LastError != "" {
		lastError = &cp.LastError
	}
	_, err := s.db.Exec(ctx, `INSERT INTO explorer.backfill_checkpoints (
			job, ceiling_height, floor_height, lowest_dispatched, blocks_done, status, last_error, started_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
		ON CONFLICT (job) DO UPDATE SET
			ceiling_height = EXCLUDED.ceiling_height, floor_height = EXCLUDED.floor_height,
			lowest_dispatched = EXCLUDED.lowest_dispatched, blocks_done = EXCLUDED.blocks_done,
			status = EXCLUDED.status, last_error = EXCLUDED.last_error, updated_at = now()`,
		cp.Job, cp.Ceiling, cp.Floor, cp.LowestDispatched, cp.BlocksDone, cp.Status, lastError)
	if err != nil {
		return fmt.Errorf("save checkpoint %s: %w", cp.Job, err)
	}
	return nil
}

// PGLocker takes session-level advisory locks on connections of their own.
type PGLocker struct {
	url string
}

// NewPGLocker returns a locker connecting to databaseURL.
func NewPGLocker(databaseURL string) *PGLocker {
	return &PGLocker{url: databaseURL}
}

// lockKey is the advisory lock key of a job.
const lockKey = `hashtextextended('bze-scan backfill ' || $1, 0)`

// TryLock implements Locker. The lock lives as long as its connection:
// release closes it, and so does a crash of the process.
func (l *PGLocker) TryLock(ctx context.Context, job string) (func(), bool, error) {
	conn, err := pgx.Connect(ctx, l.url)
	if err != nil {
		return nil, false, fmt.Errorf("connect: %w", err)
	}
	var ok bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(`+lockKey+`)`, job).Scan(&ok); err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, false, fmt.Errorf("take lock %s: %w", job, err)
	}
	if !ok {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, false, nil
	}
	return func() { _ = conn.Close(context.Background()) }, true, nil
}
