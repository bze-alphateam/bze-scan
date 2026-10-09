package writer_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// statement is one Exec the writer issued inside a transaction.
type statement struct {
	sql  string
	args []any
}

// mockTx records the statements of one transaction. pgx.Tx is embedded for
// the methods the writer never calls.
type mockTx struct {
	pgx.Tx
	db         *mockDB
	stmts      []statement
	committed  bool
	rolledBack bool
	batches    int
}

func (tx *mockTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.stmts = append(tx.stmts, statement{sql, args})
	if tx.db.execErr != nil && strings.Contains(sql, tx.db.execErrOn) {
		return pgconn.CommandTag{}, tx.db.execErr
	}
	return pgconn.CommandTag{}, nil
}

// SendBatch records the queued statements like Execs and runs their result
// callbacks over the rows configured in db.returning; Close fails when one of
// them matches the configured error.
func (tx *mockTx) SendBatch(_ context.Context, b *pgx.Batch) pgx.BatchResults {
	tx.batches++
	var err error
	for _, q := range b.QueuedQueries {
		tx.stmts = append(tx.stmts, statement{q.SQL, q.Arguments})
		if err == nil && tx.db.execErr != nil && strings.Contains(q.SQL, tx.db.execErrOn) {
			err = tx.db.execErr
		}
		if err == nil && q.Fn != nil {
			err = q.Fn(mockBatchResults{rows: tx.db.returnedBy(q.SQL)})
		}
	}
	return mockBatchResults{err: err}
}

type mockBatchResults struct {
	pgx.BatchResults
	rows [][]any
	err  error
}

func (r mockBatchResults) Close() error { return r.err }

func (r mockBatchResults) Query() (pgx.Rows, error) { return &mockRows{rows: r.rows, i: -1}, nil }

// mockRows yields canned rows of int64 or int values.
type mockRows struct {
	pgx.Rows
	rows [][]any
	i    int
}

func (r *mockRows) Next() bool {
	r.i++
	return r.i < len(r.rows)
}

func (r *mockRows) Scan(dest ...any) error {
	for j, d := range dest {
		switch d := d.(type) {
		case *int64:
			*d = int64(r.rows[r.i][j].(int))
		case *int:
			*d = r.rows[r.i][j].(int)
		}
	}
	return nil
}

func (r *mockRows) Close()     {}
func (r *mockRows) Err() error { return nil }

func (tx *mockTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *mockTx) Rollback(context.Context) error {
	if !tx.committed {
		tx.rolledBack = true
	}
	return nil
}

// mockRow scans a canned value (nil: no rows).
type mockRow struct {
	value any
	err   error
}

func (r mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.value == nil {
		return pgx.ErrNoRows
	}
	switch d := dest[0].(type) {
	case *string:
		*d = r.value.(string)
	case **int64:
		v := r.value.(int64)
		*d = &v
	}
	return nil
}

// mockDB answers QueryRow by the first matching SQL fragment in rows and
// records every transaction.
type mockDB struct {
	rows       map[string]mockRow
	returning  map[string][][]any // rows a batched statement returns, by SQL fragment
	queries    []string
	txs        []*mockTx
	execErr    error
	execErrOn  string
	beginCalls int
}

func newMockDB(lastPartition int64) *mockDB {
	return &mockDB{rows: map[string]mockRow{"pg_inherits": {value: lastPartition}}}
}

func (db *mockDB) Begin(context.Context) (pgx.Tx, error) {
	db.beginCalls++
	tx := &mockTx{db: db}
	db.txs = append(db.txs, tx)
	return tx, nil
}

func (db *mockDB) returnedBy(sql string) [][]any {
	for frag, rows := range db.returning {
		if strings.Contains(sql, frag) {
			return rows
		}
	}
	return nil
}

func (db *mockDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	db.queries = append(db.queries, sql)
	for frag, row := range db.rows {
		if strings.Contains(sql, frag) {
			return row
		}
	}
	return mockRow{}
}

func block(h int64) *transform.Entities {
	return &transform.Entities{Blocks: []transform.Block{{Height: h, Time: time.Unix(h, 0), Hash: "AB"}}}
}

func sqlOf(tx *mockTx) []string {
	var out []string
	for _, s := range tx.stmts {
		switch {
		case strings.Contains(s.sql, "ensure_partitions"):
			out = append(out, "partitions")
		case strings.Contains(s.sql, "INSERT INTO explorer.transactions"):
			out = append(out, "transactions")
		case strings.Contains(s.sql, "INSERT INTO explorer.messages"):
			out = append(out, "messages")
		case strings.Contains(s.sql, "INSERT INTO explorer.blocks"):
			out = append(out, "block")
		case strings.Contains(s.sql, "UPDATE explorer.transactions AS t"):
			out = append(out, "transactions update")
		case strings.Contains(s.sql, "UPDATE explorer.messages AS t"):
			out = append(out, "messages update")
		case strings.Contains(s.sql, "UPDATE explorer.blocks AS t"):
			out = append(out, "block update")
		case strings.Contains(s.sql, "UPDATE explorer.blocks"):
			out = append(out, "block times")
		case strings.Contains(s.sql, "UPDATE explorer.index_failures"):
			out = append(out, "resolve failures")
		case strings.Contains(s.sql, "index_failures"):
			out = append(out, "failure")
		case strings.Contains(s.sql, "DO NOTHING") && strings.Contains(s.sql, "indexer_state"):
			out = append(out, "floor")
		case strings.Contains(s.sql, "GREATEST"):
			out = append(out, "cursor")
		default:
			out = append(out, s.sql)
		}
	}
	return out
}

func TestWriteBlockIsOneTransaction(t *testing.T) {
	db := newMockDB(49) // partitions up to blocks_p000049
	w := writer.NewLiveWriter(db)

	require.NoError(t, w.WriteBlock(context.Background(), block(24_998_316)))
	require.Len(t, db.txs, 1)
	tx := db.txs[0]
	assert.Equal(t, []string{"block", "floor", "cursor"}, sqlOf(tx))
	assert.True(t, tx.committed)
	assert.Equal(t, int64(24_998_316), tx.stmts[0].args[0])
	assert.Equal(t, []any{writer.KeyLiveFloor, "24998316"}, tx.stmts[1].args)
	assert.Equal(t, []any{writer.KeyLastIndexedHeight, "24998316"}, tx.stmts[2].args)
}

func TestNothingToWrite(t *testing.T) {
	db := newMockDB(49)
	w := writer.NewLiveWriter(db)

	require.NoError(t, w.WriteBlock(context.Background(), nil))
	require.NoError(t, w.WriteBlock(context.Background(), &transform.Entities{}))
	assert.Zero(t, db.beginCalls)
}

func TestTopsUpPartitionsOnceWhenEnteringTheLast(t *testing.T) {
	db := newMockDB(49)
	w := writer.NewLiveWriter(db)
	ctx := context.Background()

	require.NoError(t, w.WriteBlock(ctx, block(48_999_999)))
	require.NoError(t, w.WriteBlock(ctx, block(49_000_000)))
	require.NoError(t, w.WriteBlock(ctx, block(49_000_001)))

	assert.Equal(t, []string{"block", "floor", "cursor"}, sqlOf(db.txs[0]))
	assert.Equal(t, []string{"partitions", "block", "floor", "cursor"}, sqlOf(db.txs[1]))
	assert.Equal(t, []any{int64(49_000_000), int64(69_000_000)}, db.txs[1].stmts[0].args)
	assert.Equal(t, []string{"block", "floor", "cursor"}, sqlOf(db.txs[2]), "topped up once")
	assert.Len(t, db.queries, 1, "the partition catalog is read once")
}

func TestTopsUpAWriteAboveEveryPartition(t *testing.T) {
	db := newMockDB(49)
	w := writer.NewLiveWriter(db)

	require.NoError(t, w.WriteBlock(context.Background(), block(75_000_000)))
	assert.Equal(t, []string{"partitions", "block", "floor", "cursor"}, sqlOf(db.txs[0]))
}

func TestFailedStatementRollsBack(t *testing.T) {
	db := newMockDB(49)
	db.execErr, db.execErrOn = errors.New("disk full"), "INSERT INTO explorer.blocks"
	w := writer.NewLiveWriter(db)

	err := w.WriteBlock(context.Background(), block(100))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
	assert.False(t, db.txs[0].committed)
	assert.True(t, db.txs[0].rolledBack)
}

func TestRecordFailureMovesTheCursorInTheSameTransaction(t *testing.T) {
	db := newMockDB(49)
	w := writer.NewLiveWriter(db)

	require.NoError(t, w.RecordFailure(context.Background(), 100, 3, errors.New("above tip")))
	require.Len(t, db.txs, 1)
	tx := db.txs[0]
	assert.Equal(t, []string{"failure", "cursor"}, sqlOf(tx))
	assert.Equal(t, []any{int64(100), writer.FailureSourceLive, 3, "above tip"}, tx.stmts[0].args)
	assert.True(t, tx.committed)
}

func TestCursorAndFloor(t *testing.T) {
	db := &mockDB{rows: map[string]mockRow{}}
	w := writer.NewLiveWriter(db)
	ctx := context.Background()

	_, ok, err := w.Cursor(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "no row: never written")

	db.rows["indexer_state"] = mockRow{value: "24998316"}
	h, ok, err := w.LiveFloor(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int64(24998316), h)

	db.rows["indexer_state"] = mockRow{value: "not a height"}
	_, _, err = w.Cursor(ctx)
	assert.Error(t, err)

	db.rows["indexer_state"] = mockRow{err: errors.New("connection reset")}
	_, _, err = w.Cursor(ctx)
	assert.Error(t, err)
}

func blockWithTxs(h int64) *transform.Entities {
	ents := block(h)
	ents.Transactions = []transform.Transaction{
		{Height: h, TxIndex: 0, Hash: "H0", Time: time.Unix(h, 0), Success: true,
			Fee: []chain.Coin{{Denom: "ubze", Amount: "5"}}, FeePayer: "bze1a", Signers: []string{"bze1a"},
			Memo: "gm", MsgCount: 1, MsgTypes: []string{"/x.MsgA"}},
		{Height: h, TxIndex: 1, Hash: "H1", Time: time.Unix(h, 0), Code: 11, Codespace: "sdk", ErrorLog: "out of gas"},
	}
	ents.Messages = []transform.Message{
		{Height: h, TxIndex: 0, MsgIndex: 0, TypeURL: "/x.MsgA", Sender: "bze1a", Module: "x",
			Events: []transform.Event{{Type: "transfer", Attrs: map[string]any{"amount": "5ubze"}}}, Body: json.RawMessage(`{"a":1}`)},
		{Height: h, TxIndex: 1, MsgIndex: 0, TypeURL: "/x.MsgB"},
	}
	return ents
}

// payload decodes the jsonb_to_recordset parameter of a bulk insert.
func payload(t *testing.T, s statement) []map[string]any {
	t.Helper()
	require.Len(t, s.args, 1)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(s.args[0].([]byte), &rows))
	return rows
}

func TestTransactionsAndMessagesJoinTheBlockTransaction(t *testing.T) {
	db := newMockDB(49)
	w := writer.NewLiveWriter(db)

	require.NoError(t, w.WriteBlock(context.Background(), blockWithTxs(100)))
	require.Len(t, db.txs, 1)
	tx := db.txs[0]
	assert.Equal(t, []string{"transactions", "messages", "block", "floor", "cursor"}, sqlOf(tx),
		"the blocks row is written last in the same transaction")
	assert.True(t, tx.committed)
	for _, st := range tx.stmts[:2] {
		assert.Contains(t, st.sql, "ON CONFLICT", "idempotent")
		assert.Contains(t, st.sql, "DO NOTHING")
	}

	txs := payload(t, tx.stmts[0])
	require.Len(t, txs, 2)
	assert.Equal(t, map[string]any{
		"height": float64(100), "tx_index": float64(0), "hash": "H0", "time": "1970-01-01T00:01:40Z",
		"success": true, "code": float64(0), "codespace": nil, "error_log": nil,
		"gas_wanted": float64(0), "gas_used": float64(0),
		"fee": []any{map[string]any{"denom": "ubze", "amount": "5"}}, "fee_payer": "bze1a",
		"signers": []any{"bze1a"}, "memo": "gm", "msg_count": float64(1), "msg_types": []any{"/x.MsgA"},
	}, withUTCTime(txs[0]))
	assert.Equal(t, "sdk", txs[1]["codespace"])
	assert.Equal(t, "out of gas", txs[1]["error_log"])
	assert.Nil(t, txs[1]["memo"], "empty strings are NULL")
	assert.Equal(t, []any{}, txs[1]["fee"], "no fee is an empty array, not NULL")
	assert.Equal(t, []any{}, txs[1]["signers"])
	assert.Equal(t, []any{}, txs[1]["msg_types"])

	msgs := payload(t, tx.stmts[1])
	require.Len(t, msgs, 2)
	assert.Equal(t, map[string]any{
		"height": float64(100), "tx_index": float64(0), "msg_index": float64(0), "type_url": "/x.MsgA",
		"sender": "bze1a", "module": "x",
		"events": []any{map[string]any{"type": "transfer", "attrs": map[string]any{"amount": "5ubze"}}},
		"body":   map[string]any{"a": float64(1)},
	}, msgs[0])
	assert.Nil(t, msgs[1]["body"], "an undecodable message has a NULL body")
	assert.Nil(t, msgs[1]["sender"])
	assert.Equal(t, []any{}, msgs[1]["events"])
	assert.Contains(t, tx.stmts[1].sql, "NULLIF(body, 'null'::jsonb)")
}

// withUTCTime normalises the time column, which encoding/json writes in the
// local zone of time.Unix.
func withUTCTime(row map[string]any) map[string]any {
	tm, err := time.Parse(time.RFC3339Nano, row["time"].(string))
	if err == nil {
		row["time"] = tm.UTC().Format(time.RFC3339)
	}
	return row
}

func TestABlockWithoutTransactionsWritesNoTransactionRows(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), block(100)))
	assert.Equal(t, []string{"block", "floor", "cursor"}, sqlOf(db.txs[0]))
}

func TestFailedTransactionsInsertRollsBackTheBlock(t *testing.T) {
	db := newMockDB(49)
	db.execErr, db.execErrOn = errors.New("deadlock"), "INSERT INTO explorer.messages"
	err := writer.NewLiveWriter(db).WriteBlock(context.Background(), blockWithTxs(100))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "messages")
	assert.Equal(t, []string{"transactions", "messages"}, sqlOf(db.txs[0]), "the blocks row is never reached")
	assert.True(t, db.txs[0].rolledBack)
}
