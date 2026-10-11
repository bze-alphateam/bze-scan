package writer_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// The SQL behaviour (sums, statuses, either walking direction) runs against
// PostgreSQL in the e2e tests; these pin what the writers send.

func fill(h int64, seq int, order, amount string) transform.OrderFill {
	return transform.OrderFill{Height: h, Seq: seq, OrderID: order, MarketID: "m", Side: "sell", Price: "1",
		Amount: amount, Maker: "bze1maker", Taker: "bze1taker", Time: time.Unix(h, 0)}
}

func dexBlock(h int64, fills ...transform.OrderFill) *transform.Entities {
	ents := block(h)
	ents.OrderFills = fills
	return ents
}

func statementsOf(tx *mockTx, sqlPart string) []statement {
	var out []statement
	for _, st := range tx.stmts {
		if strings.Contains(st.sql, sqlPart) {
			out = append(out, st)
		}
	}
	return out
}

const fillDeltaSQL = "filled_amount = o.filled_amount + d.amount"

// Only the fills an insert returned move filled_amount, merged per order
// and in order id order, whatever order the heights come in.
func TestFillDeltasCountOnlyTheInsertedFills(t *testing.T) {
	write := func(batch ...*transform.Entities) []map[string]any {
		db := newMockDB(49)
		db.returning = map[string][][]any{"INSERT INTO explorer.order_fills": {{10, 0}, {10, 2}, {12, 0}, {12, 1}}}
		require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), batch, writer.ModeInsert))
		stmts := statementsOf(db.txs[0], fillDeltaSQL)
		require.Len(t, stmts, 1)
		rows := payload(t, stmts[0])
		for _, r := range rows {
			ts, err := time.Parse(time.RFC3339Nano, r["time"].(string))
			require.NoError(t, err)
			r["time"] = ts.Unix()
		}
		return rows
	}
	h10 := dexBlock(10, fill(10, 0, "B", "5"), fill(10, 1, "B", "7"), fill(10, 2, "A", "1"))
	h12 := dexBlock(12, fill(12, 0, "B", "100000000000000000000000"), fill(12, 1, "C", "3"))
	a := write(h10, h12)
	b := write(h12, h10)
	assert.Equal(t, a, b)
	assert.Equal(t, []map[string]any{
		{"order_id": "A", "amount": "1", "height": float64(10), "time": int64(10)},
		{"order_id": "B", "amount": "100000000000000000000005", "height": float64(12), "time": int64(12)},
		{"order_id": "C", "amount": "3", "height": float64(12), "time": int64(12)},
	}, a, "seq 1 of height 10 was already stored; amounts add as integers")
}

func TestAHeightWrittenAgainMovesNoFill(t *testing.T) {
	db := newMockDB(49) // the fills insert returns no key: every fill exists
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), dexBlock(100, fill(100, 3, "A", "5"))))
	assert.Empty(t, statementsOf(db.txs[0], fillDeltaSQL))
	assert.Len(t, statementsOf(db.txs[0], "INSERT INTO explorer.order_fills"), 1)
}

func TestABadFillAmountFailsTheWrite(t *testing.T) {
	db := newMockDB(49)
	db.returning = map[string][][]any{"INSERT INTO explorer.order_fills": {{100, 3}}}
	err := writer.NewLiveWriter(db).WriteBlock(context.Background(), dexBlock(100, fill(100, 3, "A", "lots")))
	require.ErrorContains(t, err, `amount "lots"`)
}

// The order's insert reads the fills of earlier flushes, so it runs before
// this flush's fills; the cancels run after it, the fill deltas last.
func TestOrderBookStatementOrder(t *testing.T) {
	ents := dexBlock(100, fill(100, 3, "A", "5"))
	at := time.Unix(100, 0)
	ents.OrderMessages = []transform.OrderMessage{{Height: 100, MessageID: "1", TxHash: "T", Kind: "create"}}
	ents.Orders = []transform.Order{
		{OrderID: "B", Height: 100, Time: at, Amount: "9", MessageID: "1", CreatedTxHash: "T"},
		{OrderID: "A", Height: 100, Time: at, Amount: "5"},
	}
	ents.OrderCancels = []transform.OrderCancel{{OrderID: "C", Height: 100, Time: at}}

	for _, mode := range []writer.Mode{writer.ModeInsert, writer.ModeUpdate} {
		t.Run(mode.String(), func(t *testing.T) {
			db := newMockDB(49)
			db.returning = map[string][][]any{"INSERT INTO explorer.order_fills": {{100, 3}}}
			require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{ents}, mode))
			var order []string
			for _, st := range db.txs[0].stmts {
				for _, part := range []string{"INSERT INTO explorer.order_messages", "UPDATE explorer.orders AS o SET\n\t\tmarket_id",
					"INSERT INTO explorer.orders", "INSERT INTO explorer.order_fills", "status = 'cancelled'", fillDeltaSQL} {
					if strings.Contains(st.sql, part) {
						order = append(order, part)
					}
				}
			}
			want := []string{"INSERT INTO explorer.order_messages", "INSERT INTO explorer.orders",
				"INSERT INTO explorer.order_fills", "status = 'cancelled'", fillDeltaSQL}
			if mode == writer.ModeUpdate {
				want = []string{"INSERT INTO explorer.order_messages", "UPDATE explorer.orders AS o SET\n\t\tmarket_id",
					"INSERT INTO explorer.orders", "INSERT INTO explorer.order_fills", "status = 'cancelled'", fillDeltaSQL}
			}
			assert.Equal(t, want, order)

			orders := payload(t, statementsOf(db.txs[0], "INSERT INTO explorer.orders")[0])
			require.Len(t, orders, 2)
			assert.Equal(t, "A", orders[0]["order_id"], "orders in id order")
			assert.Nil(t, orders[0]["message_id"], "no message id before chain v8.2.0")
			assert.Equal(t, "T", orders[1]["created_tx_hash"])
		})
	}
}

func TestOrdersInsertReadsTheStoredFillsAndCancel(t *testing.T) {
	ents := block(100)
	ents.Orders = []transform.Order{{OrderID: "A", Height: 100, Time: time.Unix(100, 0), Amount: "5"}}
	db := newMockDB(49)
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), ents))
	sql := statementsOf(db.txs[0], "INSERT INTO explorer.orders")[0].sql
	for _, part := range []string{
		"coalesce(sum(f.amount), 0) AS filled",
		"e.type = 'bze.tradebin.OrderCanceledEvent' AND e.attrs->>'id' = r.order_id",
		"WHEN c.height IS NOT NULL THEN 'cancelled' WHEN s.filled >= r.amount THEN 'filled' ELSE 'open'",
		"ON CONFLICT (order_id) DO NOTHING",
	} {
		assert.Contains(t, sql, part)
	}
}

// One cancel per order, in order id order.
func TestCancelsAreMergedPerOrder(t *testing.T) {
	ents := block(100)
	at := time.Unix(100, 0)
	ents.OrderCancels = []transform.OrderCancel{
		{OrderID: "B", Height: 100, Time: at}, {OrderID: "A", Height: 100, Time: at}, {OrderID: "B", Height: 100, Time: at},
	}
	db := newMockDB(49)
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), ents))
	stmts := statementsOf(db.txs[0], "status = 'cancelled'")
	require.Len(t, stmts, 1)
	rows := payload(t, stmts[0])
	require.Len(t, rows, 2)
	assert.Equal(t, "A", rows[0]["order_id"])
	assert.Equal(t, "B", rows[1]["order_id"])
}

// The live writer runs its hooks in the block's transaction, after the
// blocks row and before the cursor; a failing hook rolls the block back.
func TestWriteBlockRunsThePostFlushHooksInTheTransaction(t *testing.T) {
	db := newMockDB(49)
	var order []string
	hook := func(_ context.Context, tx pgx.Tx, f *writer.Flush) error {
		assert.Same(t, db.txs[0], tx)
		assert.Equal(t, writer.ModeInsert, f.Mode)
		assert.True(t, f.Inserted.OrderFills[writer.FillKey{Height: 100, Seq: 3}], "the hook sees the inserted rows")
		order = sqlOf(db.txs[0])
		return nil
	}
	db.returning = map[string][][]any{"INSERT INTO explorer.order_fills": {{100, 3}}}
	require.NoError(t, writer.NewLiveWriter(db, hook).WriteBlock(context.Background(), dexBlock(100, fill(100, 3, "A", "5"))))
	assert.Equal(t, "block", order[len(order)-1], "after the blocks row")
	assert.Equal(t, "cursor", sqlOf(db.txs[0])[len(sqlOf(db.txs[0]))-1])

	failing := func(context.Context, pgx.Tx, *writer.Flush) error { return errors.New("attribution failed") }
	err := writer.NewLiveWriter(db, failing).WriteBlock(context.Background(), block(101))
	require.ErrorContains(t, err, "attribution failed")
	assert.False(t, db.txs[1].committed)
	assert.True(t, db.txs[1].rolledBack)
}

// AttributeOrders links the orders, then the fills, at or above the
// flush's lowest height; a flush without blocks sends nothing.
func TestAttributeOrdersFromTheFlushLowestHeight(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewBatchWriter(db, writer.AttributeOrders).Write(context.Background(),
		[]*transform.Entities{block(12), block(10), block(11)}, writer.ModeInsert))
	orders := statementsOf(db.txs[0], "UPDATE explorer.orders o SET created_tx_hash")
	fills := statementsOf(db.txs[0], "UPDATE explorer.order_fills f SET taker_tx_hash")
	require.Len(t, orders, 1)
	require.Len(t, fills, 1)
	assert.Equal(t, []any{int64(10)}, orders[0].args)
	assert.Equal(t, []any{int64(10)}, fills[0].args)
	stmts := db.txs[0].stmts
	assert.Contains(t, stmts[len(stmts)-1].sql, "UPDATE explorer.order_fills f SET taker_tx_hash", "fills last")

	tx := &mockTx{db: db}
	require.NoError(t, writer.AttributeOrders(context.Background(), tx, &writer.Flush{Entities: &transform.Entities{}}))
	require.NoError(t, writer.AttributeOrders(context.Background(), tx, nil))
	assert.Empty(t, tx.stmts)

	db.execErr, db.execErrOn = errors.New("deadlock"), "SET taker_tx_hash"
	err := writer.NewBatchWriter(db, writer.AttributeOrders).Write(context.Background(),
		[]*transform.Entities{block(13)}, writer.ModeInsert)
	require.ErrorContains(t, err, "attribute fills")
}
