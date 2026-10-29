package trades_test

import (
	"context"
	"encoding/json"
	"errors"
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
	// execs are the statements run outside a transaction.
	execs []stmt
	// failOnce fails the first statement holding it with a serialization
	// failure.
	failOnce string
	// present are the stored heights the blocks lookups answer.
	present []int64
}

// setHoles applies an indexer_state write: an insert that does nothing on a
// conflict only creates the row.
func (d *db) setHoles(sql string, args []any) {
	if strings.Contains(sql, "INSERT INTO explorer.indexer_state") &&
		(d.holes == "" || !strings.Contains(sql, "DO NOTHING")) {
		d.holes = args[1].(string)
	}
}

func (d *db) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	d.execs = append(d.execs, stmt{sql, args})
	d.setHoles(sql, args)
	return pgconn.CommandTag{}, nil
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
	log       []string // every statement, in order
	committed bool
	// holes and cursor are the writes, applied on commit.
	writes []stmt
}

func (t *tx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.queries = append(t.queries, stmt{sql, args})
	t.log = append(t.log, sql)
	return t.db.Query(ctx, sql, args...)
}

func (t *tx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	t.log = append(t.log, sql)
	if strings.Contains(sql, "indexer_state") {
		h := t.db.holes
		for _, w := range t.writes { // this transaction's own writes
			if strings.Contains(w.sql, "INSERT INTO explorer.indexer_state") && (h == "" || !strings.Contains(w.sql, "DO NOTHING")) {
				h = w.args[1].(string)
			}
		}
		if h != "" {
			return row{[]any{h}}
		}
	}
	return row{}
}

func (t *tx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, stmt{sql, args})
	t.log = append(t.log, sql)
	if f := t.db.failOnce; f != "" && strings.Contains(sql, f) {
		t.db.failOnce = ""
		return pgconn.CommandTag{}, &pgconn.PgError{Code: "40001", Message: "could not serialize access"}
	}
	t.writes = append(t.writes, stmt{sql, args})
	return pgconn.CommandTag{}, nil
}

func (t *tx) Commit(context.Context) error {
	t.committed = true
	for _, w := range t.writes {
		t.db.setHoles(w.sql, w.args)
		if strings.Contains(w.sql, "pending_rewrites SET resume_from") {
			t.db.cursor = w.args[0]
		}
	}
	return nil
}
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
	res, err := trades.CatchUp(context.Background(), d, nil)
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
	assert.JSONEq(t, `[[0,22499999]]`, d.holes, "the first run's seed, and no other hole: every height has a block")
	assert.Equal(t, int64(migrations.PartitionSize), trades.RangeSize)
}

// The cursor at the top already: nothing to number; no cursor row (a schema
// before 000023): nothing done at all.
func TestCatchUpWithNothingToDo(t *testing.T) {
	d := &db{cursor: int64(101), top: int64p(100)}
	res, err := trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	assert.Zero(t, res.Ranges)

	d = &db{}
	res, err = trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	assert.Zero(t, res.Ranges)
	assert.Empty(t, d.txs)

	d = &db{cursor: int64(0), top: (*int64)(nil)}
	_, err = trades.CatchUp(context.Background(), d, nil)
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
	res, err := trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	assert.Equal(t, trades.Result{Ranges: 1, Heights: 1}, res)
	assert.Empty(t, cursorMoves(t, d))
	require.Len(t, committed(d), 1)
	for _, q := range committed(d)[0].queries {
		assert.Equal(t, []any{int64(70), int64(71)}, q.args)
	}

	var swaps, fills []map[string]any
	for _, e := range committed(d)[0].execs {
		if len(e.args) == 0 {
			continue // SET TRANSACTION
		}
		var payload []map[string]any
		if raw, ok := e.args[0].([]byte); !ok || json.Unmarshal(raw, &payload) != nil {
			continue
		}
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
	_, err := trades.CatchUp(context.Background(), d, nil)
	require.ErrorContains(t, err, "height 7 block event 0")
	assert.Empty(t, committed(d))
}

// The cursor keeps the heights it passes without a block as holes; a later
// pass numbers the ones stored since (the previous release's backfill, with
// only pool swaps and no fill to give them away) and cuts them out.
func TestCatchUpNumbersHeightsStoredInTheHoles(t *testing.T) {
	d := &db{cursor: int64(100), top: int64p(120), present: []int64{100, 101, 105, 110, 111, 112, 120}}
	_, err := trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	assert.JSONEq(t, `[[0,99],[102,104],[106,109],[113,119]]`, d.holes)

	// The previous release stores 103, 104 and 107; the cursor is at 121.
	at := time.Unix(100, 0).UTC()
	swap := `{"creator":"bze1c","in":{"denom":"ubze","amount":"5"},"out":{"denom":"uatom","amount":"2"},"pool_id":"uatom_ubze"}`
	d.present = append(d.present, 103, 104, 107)
	d.results = map[string][][]any{"FROM explorer.block_events": {{int64(104), 0, at, []byte(swap)}}}
	d.txs = nil
	res, err := trades.CatchUp(context.Background(), d, nil)
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
	assert.JSONEq(t, `[[0,99],[102,102],[106,106],[108,109],[113,119]]`, d.holes)

	// Nothing new: the holes are read, nothing written.
	d.txs = nil
	res, err = trades.CatchUp(context.Background(), d, nil)
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
	res, err := trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Ranges)
	assert.JSONEq(t, `[]`, d.holes)
}

// Every transaction runs in one snapshot (REPEATABLE READ, its first
// statement): with READ COMMITTED a block committed between the trades read
// and the blocks lookup would be passed as present, unnumbered.
func TestCatchUpTransactionsAreRepeatableRead(t *testing.T) {
	d := &db{cursor: int64(100), top: int64p(120), present: []int64{100, 101, 103}, holes: `[[50,60]]`}
	d.present = append(d.present, 55)
	_, err := trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	require.Len(t, d.txs, 2, "the holes, the cursor's range")
	for _, x := range d.txs {
		assert.Equal(t, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ", x.log[0])
	}
}

// A serialization failure (a concurrent writer under REPEATABLE READ) runs
// the transaction again.
func TestCatchUpRetriesSerializationFailures(t *testing.T) {
	d := &db{cursor: int64(100), top: int64p(101), present: []int64{100, 101}, failOnce: "pending_rewrites SET resume_from"}
	res, err := trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Ranges)
	assert.Equal(t, int64(102), d.cursor)
	assert.Len(t, committed(d), 1)
}

// The holes row exists before any lock is taken: the pass creates it (an
// insert that keeps an existing row) before it reads, and every transaction
// that locks it inserts the empty row first, so two first passes cannot
// overwrite each other's holes.
func TestHolesRowExistsBeforeItIsLocked(t *testing.T) {
	d := &db{cursor: int64(100), top: int64p(101), present: []int64{101}}
	_, err := trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	require.NotEmpty(t, d.execs)
	assert.Contains(t, d.execs[0].sql, "ON CONFLICT (key) DO NOTHING")
	for _, x := range d.txs {
		for i, sql := range x.log {
			if strings.Contains(sql, "FOR UPDATE") {
				assert.Contains(t, x.log[i-1], "ON CONFLICT (key) DO NOTHING", "the row is created before the lock")
			}
		}
	}
	assert.JSONEq(t, `[[0,100]]`, d.holes, "the seed and the new hole at 100 merged")

	// An existing row is kept.
	d = &db{cursor: int64(100), top: int64p(99), holes: `[[7,7]]`}
	_, err = trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	assert.JSONEq(t, `[[7,7]]`, d.holes)
}

// The first run of this release (no holes row yet) with the cursor already
// up: the heights below it were passed without being recorded, so it checks
// every stored height below the cursor once, and keeps as holes only those
// without a block.
func TestFirstRunChecksEverythingBelowTheCursor(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	swap := `{"creator":"bze1c","in":{"denom":"ubze","amount":"5"},"out":{"denom":"uatom","amount":"2"},"pool_id":"uatom_ubze"}`
	d := &db{cursor: int64(500), top: int64p(499), present: []int64{70, 71, 72, 300},
		results: map[string][][]any{"FROM explorer.block_events": {{int64(71), 2, at, []byte(swap)}}}}
	res, err := trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	assert.Equal(t, trades.Result{Ranges: 1, Heights: 1}, res)
	var swaps []map[string]any
	for _, e := range committed(d)[0].execs {
		if strings.Contains(e.sql, "INSERT INTO explorer.swaps") {
			require.NoError(t, json.Unmarshal(e.args[0].([]byte), &swaps))
		}
	}
	require.Len(t, swaps, 1)
	assert.Equal(t, float64(71_000_001), swaps[0]["trade_id"])
	assert.JSONEq(t, `[[0,69],[73,299],[301,499]]`, d.holes)

	// A cursor still at 0 (the walk from genesis to come) seeds no hole.
	d = &db{cursor: int64(0), top: (*int64)(nil)}
	_, err = trades.CatchUp(context.Background(), d, nil)
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, d.holes)
}

// hook records the ranges it ran in and the transaction it got.
type hook struct {
	ranges [][2]int64
	txs    []trades.HookTx
	fail   error
}

func (h *hook) AfterNumbering(_ context.Context, tx trades.HookTx, lo, hi int64) error {
	h.ranges, h.txs = append(h.ranges, [2]int64{lo, hi}), append(h.txs, tx)
	return h.fail
}

// The hook runs in every range's transaction, after the numbering, the
// heights below the cursor one at a time first; its failure rolls the
// range back.
func TestCatchUpRunsTheHookInEveryRange(t *testing.T) {
	d := &db{cursor: int64(23_500_000), top: int64p(24_000_010), results: map[string][][]any{
		"trade_id IS NULL": {{int64(70)}},
	}}
	h := &hook{}
	_, err := trades.CatchUp(context.Background(), d, h)
	require.NoError(t, err)
	assert.Equal(t, [][2]int64{{70, 71}, {23_500_000, 24_000_000}, {24_000_000, 24_000_011}}, h.ranges)
	require.Len(t, committed(d), 3)
	for i, x := range committed(d) {
		assert.Same(t, x, h.txs[i], "the range's own transaction")
	}

	// The heights stored in the holes since: the hook runs on their runs,
	// so their candles count them too.
	d = &db{cursor: int64(100), top: int64p(99), holes: `[[50,60]]`, present: []int64{55, 56, 58}}
	h = &hook{}
	_, err = trades.CatchUp(context.Background(), d, h)
	require.NoError(t, err)
	assert.Equal(t, [][2]int64{{55, 57}, {58, 59}}, h.ranges)
	require.Len(t, committed(d), 1, "one transaction for the holes")
	assert.Same(t, committed(d)[0], h.txs[0])

	d = &db{cursor: int64(100), top: int64p(100)}
	h = &hook{fail: errors.New("candles are down")}
	_, err = trades.CatchUp(context.Background(), d, h)
	assert.ErrorIs(t, err, h.fail)
	assert.Empty(t, committed(d))
}
