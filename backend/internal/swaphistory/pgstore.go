package swaphistory

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGStore reads explorer.swaps.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// SwapsByCreator implements Store. Newest first is by height, then trade id:
// block times only grow with the height, and this order walks the
// (creator, height DESC) index.
func (s *PGStore) SwapsByCreator(ctx context.Context, creator string, limit int) ([]Swap, error) {
	rows, err := s.db.Query(ctx, `SELECT trade_id, floor(extract(epoch FROM time) * 1000)::bigint, pool_id, base, quote,
			side, base_amount::text, quote_amount::text, creator
		  FROM explorer.swaps WHERE creator = $1
		 ORDER BY height DESC, trade_id DESC LIMIT $2`, creator, limit)
	if err != nil {
		return nil, fmt.Errorf("swaps of %s: %w", creator, err)
	}
	var sw Swap
	out := []Swap{}
	_, err = pgx.ForEachRow(rows, []any{&sw.TradeID, &sw.TimeMs, &sw.PoolID, &sw.Base, &sw.Quote, &sw.Side,
		&sw.BaseAmount, &sw.QuoteAmount, &sw.Creator}, func() error {
		out = append(out, sw)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("swaps of %s: %w", creator, err)
	}
	return out, nil
}
