package writer_test

import (
	"context"
	"errors"
	"fmt"
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

	require.NoError(t, w.Write(context.Background(), []*transform.Entities{blockWithTxs(104), block(103), blockWithTxs(102)}))
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
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{ents}))
	tx := db.txs[0]
	assert.Equal(t, []string{"transactions", "transactions", "block", "block times"}, sqlOf(tx))
	assert.Len(t, payload(t, tx.stmts[0]), writer.ChunkRows)
	assert.Len(t, payload(t, tx.stmts[1]), 1)
}

func TestBatchWriteTopsUpPartitionsAboveTheHighest(t *testing.T) {
	db := newMockDB(49) // the last partition starts at 49,000,000
	w := writer.NewBatchWriter(db)
	require.NoError(t, w.Write(context.Background(), []*transform.Entities{block(48_999_998), block(48_999_999)}))
	require.NoError(t, w.Write(context.Background(), []*transform.Entities{block(49_000_001), block(48_999_999)}))
	require.NoError(t, w.Write(context.Background(), []*transform.Entities{block(49_000_002)}))

	assert.Equal(t, "block", sqlOf(db.txs[0])[0])
	assert.Equal(t, "partitions", sqlOf(db.txs[1])[0])
	assert.Equal(t, []any{int64(49_000_001), int64(69_000_001)}, db.txs[1].stmts[0].args)
	assert.Equal(t, "block", sqlOf(db.txs[2])[0], "topped up once")
}

func TestBatchWriteRunsThePostFlushHooksInTheTransaction(t *testing.T) {
	db := newMockDB(49)
	var seen []int64
	hook := func(_ context.Context, tx pgx.Tx, flushed *transform.Entities) error {
		assert.Same(t, db.txs[0], tx)
		for _, b := range flushed.Blocks {
			seen = append(seen, b.Height)
		}
		return nil
	}
	failing := func(context.Context, pgx.Tx, *transform.Entities) error { return errors.New("attribution failed") }

	require.NoError(t, writer.NewBatchWriter(db, hook).Write(context.Background(), []*transform.Entities{block(5), block(4)}))
	assert.Equal(t, []int64{5, 4}, seen)

	err := writer.NewBatchWriter(db, failing).Write(context.Background(), []*transform.Entities{block(3)})
	require.ErrorContains(t, err, "attribution failed")
	assert.False(t, db.txs[1].committed)
	assert.True(t, db.txs[1].rolledBack)
}

func TestBatchWriteErrorRollsBack(t *testing.T) {
	db := newMockDB(49)
	db.execErr, db.execErrOn = errors.New("connection reset"), "INSERT INTO explorer.blocks"
	err := writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{block(9), block(8)})
	require.ErrorContains(t, err, "flush 8..9")
	assert.True(t, db.txs[0].rolledBack)
}

func TestBatchWriteOfNothing(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), nil))
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{nil, {}}))
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
