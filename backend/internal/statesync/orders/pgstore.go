package orders

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PGStore reads, names and closes explorer.orders rows.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// Cursor reads indexer_state.last_indexed_height and its block's time.
func (s *PGStore) Cursor(ctx context.Context) (Position, bool, error) {
	var raw string
	var p Position
	err := s.db.QueryRow(ctx, `SELECT st.value, b.time FROM explorer.indexer_state st
		JOIN explorer.blocks b ON b.height = st.value::bigint WHERE st.key = 'last_indexed_height'`).Scan(&raw, &p.Time)
	if errors.Is(err, pgx.ErrNoRows) {
		return Position{}, false, nil
	}
	if err != nil {
		return Position{}, false, fmt.Errorf("orders cursor: %w", err)
	}
	if p.Height, err = strconv.ParseInt(raw, 10, 64); err != nil {
		return Position{}, false, fmt.Errorf("orders cursor: %q is not a height", raw)
	}
	p.Time = p.Time.UTC()
	return p, true, nil
}

// Open returns the open orders created at or below height.
func (s *PGStore) Open(ctx context.Context, height int64) ([]OpenOrder, error) {
	rows, err := s.db.Query(ctx, `SELECT order_id, owner, market_id, side FROM explorer.orders
		WHERE status = 'open' AND created_height <= $1 ORDER BY order_id`, height)
	if err != nil {
		return nil, fmt.Errorf("open orders: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OpenOrder, error) {
		var o OpenOrder
		err := r.Scan(&o.OrderID, &o.Owner, &o.MarketID, &o.Side)
		return o, err
	})
	if err != nil {
		return nil, fmt.Errorf("open orders: %w", err)
	}
	return out, nil
}

// Close closes the orders still open among ids as filled at pos, locking
// them in order id order like the writers do.
func (s *PGStore) Close(ctx context.Context, ids []string, pos Position) (int64, error) {
	tag, err := s.db.Exec(ctx, `WITH locked AS (SELECT order_id FROM explorer.orders
		 WHERE order_id = ANY($1) AND status = 'open' ORDER BY order_id FOR UPDATE)
		UPDATE explorer.orders SET status = 'filled', closed_height = $2, closed_time = $3, updated_at = now()
		 WHERE order_id IN (SELECT order_id FROM locked) AND status = 'open'`, ids, pos.Height, pos.Time)
	if err != nil {
		return 0, fmt.Errorf("close orders: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Name sets the owner of the orders without one among owners and the maker
// of their fills without one, in one transaction.
func (s *PGStore) Name(ctx context.Context, owners map[string]string) (int64, error) {
	ids := make([]string, 0, len(owners))
	for id := range owners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = owners[id]
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("name orders: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `WITH r AS (SELECT * FROM unnest($1::text[], $2::text[]) AS r(order_id, owner)),
		locked AS (SELECT o.order_id FROM explorer.orders o JOIN r ON r.order_id = o.order_id
		            WHERE o.owner = '' ORDER BY o.order_id FOR UPDATE OF o)
		UPDATE explorer.orders AS o SET owner = r.owner, updated_at = now()
		  FROM r WHERE o.order_id = r.order_id AND o.order_id IN (SELECT order_id FROM locked)`, ids, names)
	if err != nil {
		return 0, fmt.Errorf("name orders: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE explorer.order_fills AS f SET maker = r.owner
		  FROM unnest($1::text[], $2::text[]) AS r(order_id, owner)
		 WHERE f.order_id = r.order_id AND f.maker = ''`, ids, names); err != nil {
		return 0, fmt.Errorf("name fill makers: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("name orders: %w", err)
	}
	return tag.RowsAffected(), nil
}
