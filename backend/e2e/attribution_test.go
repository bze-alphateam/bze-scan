//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

const attH = int64(26_100_000)

// crossBlockHeights: at attH a create (message 1, tx 11) rests order 100
// in its own block, and the queue empties, so the counter restarts; at
// attH+1 another create reuses message id 1 (tx 12) and its processing
// fails, so it stays queued; attH+2 is empty; at attH+3 message 1 is
// processed (a fill of order 100 and the remainder saved as order 101)
// next to a MsgFillOrders of that block (message 2, tx 13) filling order
// 100 again.
func crossBlockHeights() []*transform.Entities {
	created := func(h int64, msg int, kind string, tx string) transform.OrderMessage {
		return transform.OrderMessage{Height: h, MessageID: oid(msg), TxHash: tx, Kind: kind}
	}
	a := dexBlock(attH, transform.Transaction{Hash: txHash(11), MsgCount: 1, MsgTypes: []string{"/bze.tradebin.MsgCreateOrder"}})
	a.OrderMessages = []transform.OrderMessage{created(attH, 1, "create", txHash(11))}
	a.Orders = []transform.Order{savedOrder(attH, 100, dexMaker, "50", 1, txHash(11))}

	b := dexBlock(attH+1, transform.Transaction{Hash: txHash(12), MsgCount: 1, MsgTypes: []string{"/bze.tradebin.MsgCreateOrder"}})
	b.OrderMessages = []transform.OrderMessage{created(attH+1, 1, "create", txHash(12))}

	c := dexBlock(attH + 2)

	d := dexBlock(attH+3, transform.Transaction{Hash: txHash(13), MsgCount: 1, MsgTypes: []string{"/bze.tradebin.MsgFillOrders"}})
	d.OrderMessages = []transform.OrderMessage{created(attH+3, 2, "fill", txHash(13))}
	d.OrderFills = []transform.OrderFill{
		takenFill(attH+3, 0, 100, "20", 1, ""),
		takenFill(attH+3, 2, 100, "10", 2, txHash(13)),
	}
	d.Orders = []transform.Order{savedOrder(attH+3, 101, dexOwner, "30", 1, "")}
	return []*transform.Entities{a, b, c, d}
}

// attributedRows are the links of every order and fill.
func attributedRows(t *testing.T, e *syncEnv) []string {
	t.Helper()
	rows := slices.Concat(
		queryStrings(t, e.db, `SELECT concat_ws(' ', 'order', order_id, coalesce(created_tx_hash, '-')) FROM explorer.orders`),
		queryStrings(t, e.db, `SELECT concat_ws(' ', 'fill', height, seq, coalesce(taker_tx_hash, '-')) FROM explorer.order_fills`),
	)
	slices.Sort(rows)
	return rows
}

func attributingBatchWriter(t *testing.T, url string) *writer.BatchWriter {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return writer.NewBatchWriter(pool, writer.AttributeOrders)
}

// An outcome processed blocks after its transaction is linked to the
// message with its id at the highest height at or below its own, whichever
// is written first and in whatever order the flushes land: never to the
// older message that used the same id, even while the heights in between
// are missing.
func TestCrossBlockAttributionInEveryOrder(t *testing.T) {
	heights := crossBlockHeights()
	ctx := context.Background()
	want := []string{
		"order " + oid(100) + " " + txHash(11),
		"order " + oid(101) + " " + txHash(12),
		"fill 26100003 0 " + txHash(12),
		"fill 26100003 2 " + txHash(13),
	}
	slices.Sort(want)

	// Live: ascending, one height per write, the message stored first.
	up := newSyncEnv(t)
	pool, err := pgxpool.New(ctx, up.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	live := writer.NewLiveWriter(pool, writer.AttributeOrders)
	base := up.serveAPI(t)
	txDex := func(n int) *dto.TxDex {
		r := fetch(t, base+"/txs/"+txHash(n))
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		return into[dto.Tx](t, r).Dex
	}
	for _, ents := range heights[:3] {
		require.NoError(t, live.WriteBlock(ctx, ents))
	}
	queued := txDex(12)
	require.NotNil(t, queued)
	assert.True(t, queued.Pending, "queued, its outcome not indexed yet")
	require.NoError(t, live.WriteBlock(ctx, heights[3]))
	assert.Equal(t, want, attributedRows(t, up), "live")

	processed := txDex(12)
	require.NotNil(t, processed)
	assert.False(t, processed.Pending, "the outcome landed three blocks later")
	require.Len(t, processed.Placed, 1)
	assert.Equal(t, oid(101), processed.Placed[0].OrderID)
	require.Len(t, processed.Fills, 1)
	assert.Equal(t, attH+3, processed.Fills[0].Height)
	older := txDex(11)
	require.NotNil(t, older)
	require.Len(t, older.Placed, 1, "the older message with the same id keeps its own outcome only")
	assert.Equal(t, oid(100), older.Placed[0].OrderID)
	assert.Empty(t, older.Fills)

	// Backfill: descending, the outcome stored first.
	down := newSyncEnv(t)
	w := attributingBatchWriter(t, down.url)
	for _, ents := range slices.Backward(heights) {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeInsert))
	}
	assert.Equal(t, want, attributedRows(t, down), "backfill")

	// Out of order: while a height in between is missing, the older
	// message with the same id must not claim the outcome; the empty
	// height that closes the gap links it.
	gaps := newSyncEnv(t)
	w = attributingBatchWriter(t, gaps.url)
	for _, i := range []int{3, 0, 1} {
		require.NoError(t, w.Write(ctx, []*transform.Entities{heights[i]}, writer.ModeInsert))
		rows := attributedRows(t, gaps)
		assert.Contains(t, rows, "order "+oid(101)+" -", "height %d written, %d still missing", heights[i].Blocks[0].Height, attH+2)
		assert.Contains(t, rows, "fill 26100003 0 -")
	}
	require.NoError(t, w.Write(ctx, []*transform.Entities{heights[2]}, writer.ModeInsert))
	assert.Equal(t, want, attributedRows(t, gaps), "the gap closed")

	// Written again in one flush, then reindexed: nothing changes.
	require.NoError(t, w.Write(ctx, heights, writer.ModeInsert))
	require.NoError(t, w.Write(ctx, heights, writer.ModeUpdate))
	assert.Equal(t, want, attributedRows(t, gaps), "a reindex changes nothing")
	assert.Equal(t, orderRows(t, down), orderRows(t, gaps))
	assert.Equal(t, orderRows(t, down), orderRows(t, up), "every walking order ends with the same rows")
}

const (
	reconcileH    = int64(26_200_000)
	marketMaker   = "bze10kw8lpqd9emyxn94gkm038t4jj90ark4x8ls0d"
	listedOrder   = 6515921 // listed in the market maker's recorded order book
	vanishedOrder = 42
	newerOrder    = 43
)

// The hourly reconciliation closes, as filled, the open orders the node's
// order book no longer lists (a remainder removed without an event), at
// the reconciliation height; rows the node lists and rows above that
// height stay open, and a second run changes nothing.
func TestReconciliationClosesTheOrdersTheNodeNoLongerLists(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	live := writer.NewLiveWriter(pool, writer.AttributeOrders)

	at := dexBlock(reconcileH)
	at.Orders = []transform.Order{
		savedOrder(reconcileH, listedOrder, marketMaker, "10", 0, ""),
		savedOrder(reconcileH, vanishedOrder, marketMaker, "10", 0, ""),
	}
	above := dexBlock(reconcileH + 1)
	above.Orders = []transform.Order{savedOrder(reconcileH+1, newerOrder, marketMaker, "10", 0, "")}
	require.NoError(t, live.WriteBlock(ctx, at))
	require.NoError(t, live.WriteBlock(ctx, above))
	// The node's tip lags the indexer: the run reads the book at the tip,
	// so an order saved above it is not compared.
	e.node.SetStatusHeight(reconcileH)

	state := func() []string {
		return queryStrings(t, e.db, `SELECT concat_ws(' ', order_id, status, coalesce(closed_height::text, '-'),
			(closed_time IS NOT NULL)::text) FROM explorer.orders`)
	}
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	want := []string{
		oid(vanishedOrder) + " filled 26200000 true",
		oid(newerOrder) + " open - false",
		oid(listedOrder) + " open - false",
	}
	slices.Sort(want)
	assert.Equal(t, want, state())
	assert.Equal(t, []string{"orders"}, queryStrings(t, e.db,
		`SELECT job FROM explorer.sync_jobs WHERE job = 'orders' AND last_error IS NULL`))
	assert.Equal(t, 1, e.grpc.RequestsFor("tradebin", "UserMarketOrders", marketMaker), "one owner, one page")

	code, out = e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, want, state(), "a second run changes nothing")
}
