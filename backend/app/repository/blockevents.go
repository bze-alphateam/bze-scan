package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ChainEvent is a stored block event with its block's time and its
// classification.
type ChainEvent struct {
	Height   int64
	Seq      int64
	Time     time.Time
	Type     string
	Kind     string
	Category string
	Attrs    json.RawMessage
}

// ChainEventKey is the keyset position of a block event in the global list.
type ChainEventKey struct {
	Height int64
	Seq    int64
}

// ChainEvents returns up to limit stored block events, newest first (height,
// then position, descending), of type typ when it is not empty, after
// before when it is not nil. A type reads the (type, height) index.
func (r *Explorer) ChainEvents(ctx context.Context, typ string, before *ChainEventKey, limit int) ([]ChainEvent, error) {
	var bh, bs *int64
	if before != nil {
		bh, bs = &before.Height, &before.Seq
	}
	rows, err := r.db.Query(ctx, `SELECT e.height, e.seq, b.time, e.type, k.kind, k.category, e.attrs
		FROM explorer.block_events e
		JOIN explorer.blocks b ON b.height = e.height
		JOIN explorer.block_event_kinds k ON k.event_type = e.type
		WHERE ($1 = '' OR e.type = $1)
		  AND ($2::bigint IS NULL OR (e.height, e.seq) < ($2::bigint, $3::integer))
		ORDER BY e.height DESC, e.seq DESC LIMIT $4`, typ, bh, bs, limit)
	if err != nil {
		return nil, fmt.Errorf("block events of %q: %w", typ, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChainEvent, error) {
		var e ChainEvent
		err := row.Scan(&e.Height, &e.Seq, &e.Time, &e.Type, &e.Kind, &e.Category, &e.Attrs)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("block events of %q: %w", typ, err)
	}
	return out, nil
}
