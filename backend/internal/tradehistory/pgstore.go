package tradehistory

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/swaphistory"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PGStore reads explorer.order_fills and explorer.swaps. Every query is
// ordered by height, then trade id, so it walks the (…, height DESC)
// indexes; a time window is bounded by the heights of its blocks
// (Heights), which prunes the partitions outside it.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

const fillColumns = `trade_id, floor(extract(epoch FROM time) * 1000)::bigint, market_id, side, price::text, amount::text,
		maker, taker`

const swapColumns = `trade_id, floor(extract(epoch FROM time) * 1000)::bigint, pool_id, base, quote, side,
		base_amount::text, quote_amount::text, creator`

// Fills implements Store. A fill the trade numbering has not reached yet
// (trade_id NULL, only while a release rolls out) is left out until it has.
// An address is the maker or the taker: two index walks, the taker's
// leaving out its own orders' fills so a self-trade counts once.
func (s *PGStore) Fills(ctx context.Context, f Filter, before int64, limit int) ([]Fill, error) {
	q := query{args: []any{limit}}
	bound(&q, f, before)
	q.where("trade_id IS NOT NULL")
	if f.MarketID != "" {
		q.where("market_id = " + q.arg(f.MarketID))
	}
	var sql string
	if f.Address == "" {
		sql = `SELECT ` + fillColumns + ` FROM explorer.order_fills WHERE ` + q.and() +
			` ORDER BY height DESC, trade_id DESC LIMIT $1`
	} else {
		a := q.arg(f.Address)
		sql = `SELECT * FROM (
			(SELECT ` + fillColumns + `, height FROM explorer.order_fills WHERE maker = ` + a + ` AND ` + q.and() + `
			  ORDER BY height DESC, trade_id DESC LIMIT $1)
			UNION ALL
			(SELECT ` + fillColumns + `, height FROM explorer.order_fills WHERE taker = ` + a + ` AND maker <> ` + a + ` AND ` + q.and() + `
			  ORDER BY height DESC, trade_id DESC LIMIT $1)) t
		 ORDER BY height DESC, trade_id DESC LIMIT $1`
	}
	rows, err := s.db.Query(ctx, sql, q.args...)
	if err != nil {
		return nil, fmt.Errorf("fills: %w", err)
	}
	var (
		fl     Fill
		height int64
	)
	dest := []any{&fl.TradeID, &fl.TimeMs, &fl.MarketID, &fl.Side, &fl.Price, &fl.Amount, &fl.Maker, &fl.Taker}
	if f.Address != "" {
		dest = append(dest, &height)
	}
	out := []Fill{}
	if _, err = pgx.ForEachRow(rows, dest, func() error {
		out = append(out, fl)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("fills: %w", err)
	}
	return out, nil
}

// Swaps implements Store: a pool's swaps, an address's as their creator.
func (s *PGStore) Swaps(ctx context.Context, f Filter, before int64, limit int) ([]swaphistory.Swap, error) {
	q := query{args: []any{limit}}
	bound(&q, f, before)
	if f.MarketID != "" {
		q.where("pool_id = " + q.arg(f.MarketID))
	}
	if f.Address != "" {
		q.where("creator = " + q.arg(f.Address))
	}
	rows, err := s.db.Query(ctx, `SELECT `+swapColumns+` FROM explorer.swaps WHERE `+q.and()+`
		 ORDER BY height DESC, trade_id DESC LIMIT $1`, q.args...)
	if err != nil {
		return nil, fmt.Errorf("swaps: %w", err)
	}
	var sw swaphistory.Swap
	out := []swaphistory.Swap{}
	if _, err = pgx.ForEachRow(rows, []any{&sw.TradeID, &sw.TimeMs, &sw.PoolID, &sw.Base, &sw.Quote, &sw.Side,
		&sw.BaseAmount, &sw.QuoteAmount, &sw.Creator}, func() error {
		out = append(out, sw)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("swaps: %w", err)
	}
	return out, nil
}

// Heights implements Store: block times never decrease, so the window's
// blocks are the heights between its first and its last block.
func (s *PGStore) Heights(ctx context.Context, fromMs, toMs int64) (lo, hi int64, ok bool, err error) {
	var l, h *int64
	err = s.db.QueryRow(ctx, `SELECT
		(SELECT height FROM explorer.blocks WHERE time >= to_timestamp($1::bigint / 1000.0) ORDER BY time ASC LIMIT 1),
		(SELECT height FROM explorer.blocks WHERE time < to_timestamp(($2::bigint + 1) / 1000.0) ORDER BY time DESC LIMIT 1)`,
		fromMs, toMs).Scan(&l, &h)
	if err != nil {
		return 0, 0, false, fmt.Errorf("heights of %d..%d: %w", fromMs, toMs, err)
	}
	if l == nil || h == nil || *l > *h {
		return 0, 0, false, nil
	}
	return *l, *h, true, nil
}

// bound adds the side, the window's heights and the before cursor.
func bound(q *query, f Filter, before int64) {
	if f.Sided() {
		q.where("side = " + q.arg(f.Side))
	}
	if f.FromHeight > 0 && f.ToHeight > 0 {
		q.where("height BETWEEN " + q.arg(f.FromHeight) + " AND " + q.arg(f.ToHeight))
	}
	if before > 0 {
		q.where("height <= " + q.arg(before/transform.TradeIDsPerBlock))
		q.where("trade_id < " + q.arg(before))
	}
}

// query collects the conditions and arguments of a statement.
type query struct {
	conds []string
	args  []any
}

func (q *query) arg(v any) string {
	q.args = append(q.args, v)
	return fmt.Sprintf("$%d", len(q.args))
}

func (q *query) where(cond string) { q.conds = append(q.conds, cond) }

func (q *query) and() string {
	if len(q.conds) == 0 {
		return "TRUE"
	}
	return strings.Join(q.conds, " AND ")
}
