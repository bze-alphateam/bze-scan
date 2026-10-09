package writer_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

func TestBatchWriteIsOneTransactionOfOneBatch(t *testing.T) {
	db := newMockDB(49)
	w := writer.NewBatchWriter(db)

	require.NoError(t, w.Write(context.Background(), []*transform.Entities{blockWithTxs(104), block(103), blockWithTxs(102)}, writer.ModeInsert))
	require.Len(t, db.txs, 1)
	tx := db.txs[0]
	assert.Equal(t, 1, tx.batches)
	assert.True(t, tx.committed)
	assert.Equal(t, []string{"transactions", "messages", "block", "block times"}, sqlOf(tx),
		"never the live floor or the cursor")

	assert.Len(t, payload(t, tx.stmts[0]), 4, "both heights' transactions in one insert")
	blocks := payload(t, tx.stmts[2])
	require.Len(t, blocks, 3)
	assert.InDelta(t, 104, blocks[0]["height"], 0, "arrival order")
	assert.Nil(t, blocks[0]["fees_distributed"])
	assert.Contains(t, tx.stmts[2].sql, "NULLIF(fees_distributed, 'null'::jsonb)")
	assert.Contains(t, tx.stmts[2].sql, "ON CONFLICT (height) DO NOTHING")
	assert.Equal(t, []any{int64(102), int64(105)}, tx.stmts[3].args,
		"block times from the lowest height to one above the highest")
}

func TestBatchWriteChunksLargeInserts(t *testing.T) {
	db := newMockDB(49)
	ents := block(100)
	for i := range writer.ChunkRows + 1 {
		ents.Transactions = append(ents.Transactions, transform.Transaction{Height: 100, TxIndex: i, Hash: fmt.Sprint(i)})
	}
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{ents}, writer.ModeInsert))
	tx := db.txs[0]
	assert.Equal(t, []string{"transactions", "transactions", "block", "block times"}, sqlOf(tx))
	assert.Len(t, payload(t, tx.stmts[0]), writer.ChunkRows)
	assert.Len(t, payload(t, tx.stmts[1]), 1)
}

func TestBatchWriteTopsUpPartitionsAboveTheHighest(t *testing.T) {
	db := newMockDB(49) // the last partition starts at 49,000,000
	w := writer.NewBatchWriter(db)
	require.NoError(t, w.Write(context.Background(), []*transform.Entities{block(48_999_998), block(48_999_999)}, writer.ModeInsert))
	require.NoError(t, w.Write(context.Background(), []*transform.Entities{block(49_000_001), block(48_999_999)}, writer.ModeInsert))
	require.NoError(t, w.Write(context.Background(), []*transform.Entities{block(49_000_002)}, writer.ModeInsert))

	assert.Equal(t, "block", sqlOf(db.txs[0])[0])
	assert.Equal(t, "partitions", sqlOf(db.txs[1])[0])
	assert.Equal(t, []any{int64(49_000_001), int64(69_000_001)}, db.txs[1].stmts[0].args)
	assert.Equal(t, "block", sqlOf(db.txs[2])[0], "topped up once")
}

func TestBatchWriteRunsThePostFlushHooksInTheTransaction(t *testing.T) {
	db := newMockDB(49)
	var seen []int64
	hook := func(_ context.Context, tx pgx.Tx, f *writer.Flush) error {
		assert.Same(t, db.txs[0], tx)
		assert.Equal(t, writer.ModeInsert, f.Mode)
		for _, b := range f.Entities.Blocks {
			seen = append(seen, b.Height)
		}
		return nil
	}
	failing := func(context.Context, pgx.Tx, *writer.Flush) error { return errors.New("attribution failed") }

	require.NoError(t, writer.NewBatchWriter(db, hook).Write(context.Background(), []*transform.Entities{block(5), block(4)}, writer.ModeInsert))
	assert.Equal(t, []int64{5, 4}, seen)

	err := writer.NewBatchWriter(db, failing).Write(context.Background(), []*transform.Entities{block(3)}, writer.ModeInsert)
	require.ErrorContains(t, err, "attribution failed")
	assert.False(t, db.txs[1].committed)
	assert.True(t, db.txs[1].rolledBack)
}

func TestBatchWriteErrorRollsBack(t *testing.T) {
	db := newMockDB(49)
	db.execErr, db.execErrOn = errors.New("connection reset"), "INSERT INTO explorer.blocks"
	err := writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{block(9), block(8)}, writer.ModeInsert)
	require.ErrorContains(t, err, "flush 8..9")
	assert.True(t, db.txs[0].rolledBack)
}

func TestBatchWriteOfNothing(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), nil, writer.ModeInsert))
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{nil, {}}, writer.ModeInsert))
	assert.Zero(t, db.beginCalls)
}

func TestBatchRecordFailureNeverMovesTheCursor(t *testing.T) {
	db := newMockDB(49)
	w := writer.NewBatchWriter(db)
	require.NoError(t, w.RecordFailure(context.Background(), 319, writer.FailureSourceBackfill, 4, errors.New("above tip")))
	tx := db.txs[0]
	assert.Equal(t, []string{"failure"}, sqlOf(tx))
	assert.Equal(t, []any{int64(319), "backfill", 4, "above tip"}, tx.stmts[0].args)
	assert.True(t, tx.committed)
}

func TestAdvanceCursor(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewBatchWriter(db).AdvanceCursor(context.Background(), 500))
	assert.Equal(t, []string{"cursor"}, sqlOf(db.txs[0]))
	assert.True(t, db.txs[0].committed)
}

// Every table the batch writer fills, with the SQL fragment of its
// statements and the column a reindex must be able to change.
var writtenTables = []struct {
	label, name, keys, column string
}{
	{"transactions", "explorer.transactions", "height, tx_index", "gas_used"},
	{"messages", "explorer.messages", "height, tx_index, msg_index", "events"},
	{"block", "explorer.blocks", "height", "tx_count"},
}

func TestBatchWriteInUpdateModeOverwritesEveryTable(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(),
		[]*transform.Entities{blockWithTxs(104), block(103)}, writer.ModeUpdate))
	tx := db.txs[0]
	assert.Equal(t, []string{
		"transactions update", "transactions", "messages update", "messages",
		"block update", "block", "block times", "resolve failures",
	}, sqlOf(tx), "each table's update before its insert, both in the flush's transaction")

	for _, tc := range writtenTables {
		t.Run(tc.label, func(t *testing.T) {
			var update, insert string
			for _, st := range tx.stmts {
				if strings.Contains(st.sql, "UPDATE "+tc.name+" AS t") {
					update = st.sql
				}
				if strings.Contains(st.sql, "INSERT INTO "+tc.name) {
					insert = st.sql
				}
			}
			assert.Contains(t, update, "SET (", "overwrites the data columns")
			assert.Contains(t, update, tc.column)
			assert.Contains(t, update, ") IS DISTINCT FROM (", "identical rows are left alone")
			assert.Contains(t, insert, "ON CONFLICT ("+tc.keys+") DO NOTHING",
				"an existing row is never inserted again")
			assert.Contains(t, insert, "RETURNING "+tc.keys, "the inserted keys drive the counters")
		})
	}
	assert.Equal(t, []any{[]int64{104, 103}}, tx.stmts[len(tx.stmts)-1].args, "the flush's heights are resolved")
}

func TestBatchWriteInInsertModeNeverUpdates(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(),
		[]*transform.Entities{blockWithTxs(104)}, writer.ModeInsert))
	for _, st := range db.txs[0].stmts {
		assert.NotContains(t, st.sql, "AS t\n", "no overwrite")
		assert.NotContains(t, st.sql, "index_failures", "the backfill resolves nothing")
	}
}

func TestBatchWriteRejectsAnUnknownMode(t *testing.T) {
	db := newMockDB(49)
	err := writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{block(1)}, writer.Mode(7))
	require.ErrorContains(t, err, "unsupported mode(7)")
	assert.Zero(t, db.beginCalls)
}

// The hooks see exactly the rows the inserts returned: a first write
// inserts everything, a second write of the same heights inserts nothing,
// so a counter driven by Inserted moves once whatever the mode.
func TestPostFlushSeesOnlyTheInsertedRows(t *testing.T) {
	for _, mode := range []writer.Mode{writer.ModeInsert, writer.ModeUpdate} {
		t.Run(mode.String(), func(t *testing.T) {
			var seen []writer.Inserted
			hook := func(_ context.Context, _ pgx.Tx, f *writer.Flush) error {
				seen = append(seen, f.Inserted)
				return nil
			}
			batch := []*transform.Entities{blockWithTxs(104)}

			first := newMockDB(49)
			first.returning = map[string][][]any{
				"INSERT INTO explorer.blocks":       {{104}},
				"INSERT INTO explorer.transactions": {{104, 0}, {104, 1}},
				"INSERT INTO explorer.messages":     {{104, 0, 0}, {104, 1, 0}},
			}
			require.NoError(t, writer.NewBatchWriter(first, hook).Write(context.Background(), batch, mode))
			second := newMockDB(49) // every key present: the inserts return nothing
			require.NoError(t, writer.NewBatchWriter(second, hook).Write(context.Background(), batch, mode))

			require.Len(t, seen, 2)
			assert.Equal(t, map[int64]bool{104: true}, seen[0].Blocks)
			assert.Equal(t, map[writer.TxKey]bool{{Height: 104, TxIndex: 0}: true, {Height: 104, TxIndex: 1}: true},
				seen[0].Transactions)
			assert.Len(t, seen[0].Messages, 2)
			assert.Empty(t, seen[1].Blocks)
			assert.Empty(t, seen[1].Transactions)
			assert.Empty(t, seen[1].Messages)
		})
	}
}
