package writer_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
}

func (tx *mockTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.stmts = append(tx.stmts, statement{sql, args})
	if tx.db.execErr != nil && strings.Contains(sql, tx.db.execErrOn) {
		return pgconn.CommandTag{}, tx.db.execErr
	}
	return pgconn.CommandTag{}, nil
}

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
		case strings.Contains(s.sql, "INSERT INTO explorer.blocks"):
			out = append(out, "block")
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
