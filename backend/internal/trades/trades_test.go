package trades_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/trades"
	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

// The SQL runs against PostgreSQL in the e2e tests; these pin the walk: the
// ranges, the cursor, what is read and written.

type row struct{ values []any }

func (r row) Scan(dest ...any) error {
	if r.values == nil {
		return pgx.ErrNoRows
	}
	for i, d := range dest {
		reflect.ValueOf(d).Elem().Set(reflect.ValueOf(r.values[i]))
	}
	return nil
}

type rows struct {
	pgx.Rows
	data [][]any
	i    int
}

func (r *rows) Next() bool                    { r.i++; return r.i <= len(r.data) }
func (r *rows) Err() error                    { return nil }
func (r *rows) Close()                        {}
func (r *rows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }
func (r *rows) Scan(dest ...any) error        { return row{r.data[r.i-1]}.Scan(dest...) }

type stmt struct {
	sql  string
	args []any
}

// db answers the cursor and the highest block, and every query from results
// by SQL fragment; each transaction records what it ran.
type db struct {
	cursor, top any
	results     map[string][][]any
	txs         []*tx
}

func (d *db) Begin(context.Context) (pgx.Tx, error) {
	t := &tx{db: d}
	d.txs = append(d.txs, t)
	return t, nil
}

func (d *db) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "pending_rewrites"):
		if d.cursor == nil {
			return row{}
		}
		return row{[]any{d.cursor}}
	case strings.Contains(sql, "max(height)"):
		return row{[]any{d.top}}
	}
	return row{}
}

func (d *db) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	for part, data := range d.results {
		if strings.Contains(sql, part) {
			return &rows{data: data}, nil
		}
	}
	return &rows{}, nil
}

type tx struct {
	pgx.Tx
	db        *db
	queries   []stmt
	execs     []stmt
	committed bool
}

func (t *tx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.queries = append(t.queries, stmt{sql, args})
	return t.db.Query(ctx, sql, args...)
}

func (t *tx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, stmt{sql, args})
	return pgconn.CommandTag{}, nil
}

func (t *tx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *tx) Rollback(context.Context) error { return nil }

func int64p(v int64) *int64 { return &v }

func cursorMoves(t *testing.T, d *db) []any {
	t.Helper()
	var out []any
	for _, x := range d.txs {
		require.True(t, x.committed)
		for _, e := range x.execs {
			if strings.Contains(e.sql, "pending_rewrites SET resume_from") {
				out = append(out, e.args[0])
			}
		}
	}
	return out
}

// From the cursor to the highest block, one transaction per partition-sized
// range, each moving the cursor.
func TestCatchUpWalksPartitionByPartition(t *testing.T) {
	d := &db{cursor: int64(22_500_000), top: int64p(24_000_010)}
	res, err := trades.CatchUp(context.Background(), d)
	require.NoError(t, err)
	assert.Equal(t, 3, res.Ranges)
	assert.Equal(t, []any{int64(23_000_000), int64(24_000_000), int64(24_000_011)}, cursorMoves(t, d))
	for i, want := range [][]any{{int64(22_500_000), int64(23_000_000)}, {int64(23_000_000), int64(24_000_000)},
		{int64(24_000_000), int64(24_000_011)}} {
		require.Len(t, d.txs[i].queries, 5, "transaction swaps, block swaps, fills, the candles' pending build and trades")
		for _, q := range d.txs[i].queries {
			if !strings.Contains(q.sql, "pending_rewrites") {
				assert.Equal(t, want, q.args, "range %d", i)
			}
		}
	}
	assert.Equal(t, int64(migrations.PartitionSize), trades.RangeSize)
}

// The cursor at the top already: nothing to number; no cursor row (a schema
// before 000023): nothing done at all.
func TestCatchUpWithNothingToDo(t *testing.T) {
	d := &db{cursor: int64(101), top: int64p(100)}
	res, err := trades.CatchUp(context.Background(), d)
	require.NoError(t, err)
	assert.Zero(t, res.Ranges)

	d = &db{}
	res, err = trades.CatchUp(context.Background(), d)
	require.NoError(t, err)
	assert.Zero(t, res.Ranges)
	assert.Empty(t, d.txs)

	d = &db{cursor: int64(0), top: (*int64)(nil)}
	_, err = trades.CatchUp(context.Background(), d)
	require.NoError(t, err)
	assert.Empty(t, d.txs, "no block stored yet")
}

// Fills left without a trade id below the cursor (written by the previous
// release after the cursor passed them) are numbered one height at a time,
// the cursor left alone; the block's swaps and fills are numbered together.
func TestCatchUpNumbersFillsBelowTheCursor(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	swap := `{"creator":"bze1c","in":{"denom":"ubze","amount":"5"},"out":{"denom":"uatom","amount":"2"},"pool_id":"uatom_ubze"}`
	d := &db{cursor: int64(500), top: int64p(499), results: map[string][][]any{
		"trade_id IS NULL":                             {{int64(70)}},
		"FROM explorer.block_events":                   {{int64(70), 3, at, []byte(swap)}},
		"SELECT height, seq FROM explorer.order_fills": {{int64(70), 1}, {int64(70), 5}},
	}}
	res, err := trades.CatchUp(context.Background(), d)
	require.NoError(t, err)
	assert.Equal(t, trades.Result{Ranges: 1, Heights: 1}, res)
	assert.Empty(t, cursorMoves(t, d))
	require.Len(t, d.txs, 1)
	for _, q := range d.txs[0].queries {
		if !strings.Contains(q.sql, "pending_rewrites") {
			assert.Equal(t, []any{int64(70), int64(71)}, q.args)
		}
	}

	var swaps, fills []map[string]any
	for _, e := range d.txs[0].execs {
		var payload []map[string]any
		require.NoError(t, json.Unmarshal(e.args[0].([]byte), &payload))
		switch {
		case strings.Contains(e.sql, "INSERT INTO explorer.swaps"):
			swaps = payload
		case strings.Contains(e.sql, "UPDATE explorer.order_fills"):
			fills = payload
		}
	}
	require.Len(t, swaps, 1)
	assert.Equal(t, float64(70_000_002), swaps[0]["trade_id"], "between the fills by seq")
	assert.Equal(t, "uatom", swaps[0]["base"])
	assert.Equal(t, "buy", swaps[0]["side"])
	assert.Equal(t, float64(3), swaps[0]["block_seq"])
	assert.Equal(t, []map[string]any{
		{"height": float64(70), "seq": float64(1), "trade_id": float64(70_000_001)},
		{"height": float64(70), "seq": float64(5), "trade_id": float64(70_000_003)},
	}, fills)
}

// A stored swap that is no swap fails the range, rolled back.
func TestCatchUpFailsOnABadStoredSwap(t *testing.T) {
	d := &db{cursor: int64(0), top: int64p(10), results: map[string][][]any{
		"FROM explorer.block_events": {{int64(7), 0, time.Unix(1, 0), []byte(`{"in":{"denom":"ubze","amount":"x"}}`)}},
	}}
	_, err := trades.CatchUp(context.Background(), d)
	require.ErrorContains(t, err, "height 7 block event 0")
	assert.False(t, d.txs[0].committed)
}

// The trades numbered count in their candles from then on: their days are
// recomputed in the range's transaction, unless the post-migration build of
// the whole history is still to come.
func TestRangeRecomputesTheCandlesOfTheNumberedTrades(t *testing.T) {
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	for _, pending := range []bool{false, true} {
		d := &db{cursor: int64(70), top: int64p(70), results: map[string][][]any{
			"SELECT EXISTS": {{pending}},
			"date_bin":      {{"uatom_ubze", at}},
			"NULL::text":    {{int64(70_000_001), at, (*string)(nil), "2", func() *string { s := "5"; return &s }()}},
		}}
		_, err := trades.CatchUp(context.Background(), d)
		require.NoError(t, err)
		require.Len(t, d.txs, 1)
		var candles []map[string]any
		for _, e := range d.txs[0].execs {
			if strings.Contains(e.sql, "INSERT INTO explorer.candles") {
				require.NoError(t, json.Unmarshal(e.args[1].([]byte), &candles))
			}
		}
		if pending {
			assert.Empty(t, candles, "the build covers them")
			continue
		}
		require.Len(t, candles, 5, "one candle per length")
		assert.Equal(t, "2.500000000000000000", candles[0]["close"])
		assert.Equal(t, float64(70_000_001), candles[0]["last_trade_id"])
	}
}
