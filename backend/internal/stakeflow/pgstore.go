package stakeflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Beginner opens a transaction; *pgxpool.Pool satisfies it.
type Beginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// PGStore reads the snapshot from the explorer tables.
type PGStore struct {
	db Beginner
}

// NewPGStore returns a Store reading through db.
func NewPGStore(db Beginner) *PGStore {
	return &PGStore{db: db}
}

// Spans from the last block before the window's start (when there is one)
// up to the newest: consecutive heights share height - row_number().
const spansSQL = `
SELECT min(height), max(height), min(time), max(time)
FROM (
	SELECT height, time, height - row_number() OVER (ORDER BY height) AS run
	FROM explorer.blocks
	WHERE height >= coalesce(
		(SELECT height FROM explorer.blocks WHERE time < $1 ORDER BY time DESC, height DESC LIMIT 1),
		(SELECT min(height) FROM explorer.blocks))
) b
GROUP BY run
ORDER BY 1`

// The window's messages with a movement event. Events, not type URLs: authz
// MsgExec delegations emit the same events.
const messagesSQL = `
SELECT m.height, m.tx_index, m.msg_index, t.hash, t.time, m.events
FROM explorer.messages m
JOIN explorer.transactions t USING (height, tx_index)
WHERE m.height >= $1 AND t.time >= $2 AND t.success
	AND EXISTS (SELECT 1 FROM jsonb_array_elements(m.events) e WHERE e->>'type' = ANY($3))
ORDER BY m.height, m.tx_index, m.msg_index`

// Load implements Store: one repeatable-read transaction, so the spans and
// the messages describe the same heights.
func (s *PGStore) Load(ctx context.Context, window time.Duration) (*Snapshot, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var newest time.Time
	err = tx.QueryRow(ctx, `SELECT time FROM explorer.blocks ORDER BY time DESC, height DESC LIMIT 1`).Scan(&newest)
	if errors.Is(err, pgx.ErrNoRows) {
		return &Snapshot{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("newest block: %w", err)
	}
	cutoff := newest.Add(-window)

	snap := &Snapshot{}
	rows, err := tx.Query(ctx, spansSQL, cutoff)
	if err != nil {
		return nil, fmt.Errorf("spans: %w", err)
	}
	snap.Spans, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Span, error) {
		var sp Span
		err := r.Scan(&sp.Lo, &sp.Hi, &sp.LoTime, &sp.HiTime)
		return sp, err
	})
	if err != nil {
		return nil, fmt.Errorf("spans: %w", err)
	}
	if len(snap.Spans) == 0 {
		return snap, nil
	}
	// The first height whose time is inside the window bounds the scan, so
	// only the window's partitions are read.
	var from int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(min(height), 0) FROM explorer.blocks WHERE time >= $1`, cutoff).Scan(&from); err != nil {
		return nil, fmt.Errorf("window start: %w", err)
	}
	rows, err = tx.Query(ctx, messagesSQL, from, cutoff, EventTypes)
	if err != nil {
		return nil, fmt.Errorf("messages: %w", err)
	}
	snap.Messages, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (StoredMessage, error) {
		var m StoredMessage
		err := r.Scan(&m.Ref.Height, &m.Ref.TxIndex, &m.Ref.MsgIndex, &m.Ref.TxHash, &m.Ref.Time, &m.Events)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("messages: %w", err)
	}
	return snap, nil
}
