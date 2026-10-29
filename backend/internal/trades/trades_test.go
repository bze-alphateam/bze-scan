package trades_test

import (
	"context"
	"encoding/json"
	"fmt"
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
	// holes is the indexer_state value of trades.HolesKey; "" when absent.
	holes string
	// present are the stored heights the blocks lookups answer.
	present []int64
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

func (d *db) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "SELECT height FROM explorer.blocks") {
		lo, hi, limit := args[0].(int64), args[1].(int64), args[2].(int)
		var data [][]any
		for _, h := range d.present {
			if h >= lo && h <= hi && len(data) < limit {
				data = append(data, []any{h})
			}
		}
		return &rows{data: data}, nil
	}
	for part, data := range d.results {
		if !strings.Contains(sql, part) {
			continue
		}
		if len(args) != 2 {
			return &rows{data: data}, nil
		}
		// A range read: the rows whose height (first column) is in it.
		var in [][]any
		for _, r := range data {
			if h := r[0].(int64); h >= args[0].(int64) && h < args[1].(int64) {
				in = append(in, r)
			}
		}
		return &rows{data: in}, nil
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

func (t *tx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if strings.Contains(sql, "indexer_state") && t.db.holes != "" {
		return row{[]any{t.db.holes}}
	}
	return row{}
}

func (t *tx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, stmt{sql, args})
	if strings.Contains(sql, "INSERT INTO explorer.indexer_state") {
		t.db.holes = args[1].(string)
	}
	if strings.Contains(sql, "pending_rewrites SET resume_from") {
		t.db.cursor = args[0]
	}
	return pgconn.CommandTag{}, nil
}

func (t *tx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *tx) Rollback(context.Context) error { return nil }

func int64p(v int64) *int64 { return &v }

// committed are the transactions that wrote: a holes check with nothing to
// take out rolls back.
func committed(d *db) []*tx {
	var out []*tx
	for _, x := range d.txs {
		if x.committed {
			out = append(out, x)
		}
	}
	return out
}

func cursorMoves(t *testing.T, d *db) []any {
	t.Helper()
	var out []any
	for _, x := range committed(d) {
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
	for h := int64(22_500_000); h <= 24_000_010; h++ {
		d.present = append(d.present, h)
	}
	res, err := trades.CatchUp(context.Background(), d)
	require.NoError(t, err)
	assert.Equal(t, 3, res.Ranges)
	assert.Equal(t, []any{int64(23_000_000), int64(24_000_000), int64(24_000_011)}, cursorMoves(t, d))
	for i, want := range [][]any{{int64(22_500_000), int64(23_000_000)}, {int64(23_000_000), int64(24_000_000)},
		{int64(24_000_000), int64(24_000_011)}} {
		x := committed(d)[i]
		require.Len(t, x.queries, 4, "transaction swaps, block swaps, fills, the blocks")
		for _, q := range x.queries[:3] {
			assert.Equal(t, want, q.args, "range %d", i)
		}
	}
	assert.Empty(t, d.holes, "no height without a block: no hole")
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
	assert.Empty(t, committed(d), "no block stored yet")
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
	require.Len(t, committed(d), 1)
	for _, q := range committed(d)[0].queries {
		assert.Equal(t, []any{int64(70), int64(71)}, q.args)
	}

	var swaps, fills []map[string]any
	for _, e := range committed(d)[0].execs {
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
	assert.Empty(t, committed(d))
}

// The cursor keeps the heights it passes without a block as holes; a later
// pass numbers the ones stored since (the previous release's backfill, with
// only pool swaps and no fill to give them away) and cuts them out.
func TestCatchUpNumbersHeightsStoredInTheHoles(t *testing.T) {
	d := &db{cursor: int64(100), top: int64p(120), present: []int64{100, 101, 105, 110, 111, 112, 120}}
	_, err := trades.CatchUp(context.Background(), d)
	require.NoError(t, err)
	assert.JSONEq(t, `[[102,104],[106,109],[113,119]]`, d.holes)

	// The previous release stores 103, 104 and 107; the cursor is at 121.
	at := time.Unix(100, 0).UTC()
	swap := `{"creator":"bze1c","in":{"denom":"ubze","amount":"5"},"out":{"denom":"uatom","amount":"2"},"pool_id":"uatom_ubze"}`
	d.present = append(d.present, 103, 104, 107)
	d.results = map[string][][]any{"FROM explorer.block_events": {{int64(104), 0, at, []byte(swap)}}}
	d.txs = nil
	res, err := trades.CatchUp(context.Background(), d)
	require.NoError(t, err)
	assert.Equal(t, trades.Result{Ranges: 1, Heights: 1}, res, "one transaction for the holes, the cursor at the top")
	require.Len(t, committed(d), 1)
	var ranges [][]any
	for _, q := range committed(d)[0].queries {
		if strings.Contains(q.sql, "block_events") {
			ranges = append(ranges, q.args)
		}
	}
	assert.Equal(t, [][]any{{int64(103), int64(105)}, {int64(107), int64(108)}}, ranges, "the runs of new heights")
	var swaps []map[string]any
	for _, e := range committed(d)[0].execs {
		if strings.Contains(e.sql, "INSERT INTO explorer.swaps") {
			require.NoError(t, json.Unmarshal(e.args[0].([]byte), &swaps))
		}
	}
	require.Len(t, swaps, 1)
	assert.Equal(t, float64(104_000_001), swaps[0]["trade_id"])
	assert.JSONEq(t, `[[102,102],[106,106],[108,109],[113,119]]`, d.holes)

	// Nothing new: the holes are read, nothing written.
	d.txs = nil
	res, err = trades.CatchUp(context.Background(), d)
	require.NoError(t, err)
	assert.Zero(t, res.Ranges)
	assert.Empty(t, committed(d))
}

// Holes are taken out HoleBatch heights per transaction, the unexamined
// rest kept.
func TestCatchUpFillsBigHolesInBatches(t *testing.T) {
	n := trades.HoleBatch + 10
	d := &db{cursor: int64(n + 1), top: int64p(int64(n)), holes: fmt.Sprintf(`[[1,%d]]`, n)}
	for h := int64(1); h <= int64(n); h++ {
		d.present = append(d.present, h)
	}
	res, err := trades.CatchUp(context.Background(), d)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Ranges)
	assert.JSONEq(t, `[]`, d.holes)
}
