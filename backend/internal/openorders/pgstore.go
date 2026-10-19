package openorders

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PGStore reads the open orders from explorer.orders.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// OpenOrders reads the open orders of owner (idx_orders_open_owner) and the
// live indexer's cursor in one read-only snapshot. Amount is amount −
// filled_amount; a row with nothing left is skipped.
func (s *PGStore) OpenOrders(ctx context.Context, owner string) ([]Order, int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("open orders: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return nil, 0, fmt.Errorf("open orders: %w", err)
	}

	var raw string
	err = tx.QueryRow(ctx,
		`SELECT value FROM explorer.indexer_state WHERE key = 'last_indexed_height'`).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, fmt.Errorf("open orders cursor: %w", err)
	}
	var height int64
	if raw != "" {
		if height, err = strconv.ParseInt(raw, 10, 64); err != nil {
			return nil, 0, fmt.Errorf("open orders cursor %q: %w", raw, err)
		}
	}

	rows, err := tx.Query(ctx, `SELECT order_id, market_id, side, (amount - filled_amount)::text, price::text
		FROM explorer.orders WHERE owner = $1 AND status = 'open' AND amount > filled_amount
		ORDER BY order_id`, owner)
	if err != nil {
		return nil, 0, fmt.Errorf("open orders: %w", err)
	}
	orders, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Order, error) {
		var o Order
		err := row.Scan(&o.ID, &o.MarketID, &o.Side, &o.Amount, &o.Price)
		return o, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("open orders: %w", err)
	}
	return orders, height, nil
}
