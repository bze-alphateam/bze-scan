//go:build e2e

package e2e_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/archive"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Recorded heights with order book events: 24998316 saves eleven orders
// that 24998321 cancels, 20230045 (chain v7) saves an order that a buy of
// the same block fills, the others save, fill or cancel orders saved
// before the fixtures.
var dexHeights = []int64{legacyHeight, 24998316, 24998321, 24998326, hFailed, hOrders}

// transformed returns the entities of the recorded height h, through the
// archive adapter for an old generation as on the backfill path.
func transformed(t *testing.T, e *syncEnv, h int64) *transform.Entities {
	t.Helper()
	ctx := context.Background()
	client := node.New(e.node.URL)
	b, _, err := client.Block(ctx, h)
	require.NoError(t, err)
	r, _, err := client.BlockResults(ctx, h)
	require.NoError(t, err)
	c, _, err := client.Commit(ctx, h)
	require.NoError(t, err)
	in := transform.Input{Block: b, Results: r, Commit: c}
	if archive.GenerationAt(h).Format != archive.FormatCurrent {
		codec, err := chain.NewCodec()
		require.NoError(t, err)
		require.NoError(t, archive.New(codec).Adapt(h, &in))
	}
	ents, err := newTransformer(t).Transform(in)
	require.NoError(t, err)
	return ents
}

func batchWriter(t *testing.T, url string) *writer.BatchWriter {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return writer.NewBatchWriter(pool)
}

// orderRows are the orders rows but updated_at.
func orderRows(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', order_id, market_id, owner, side, price, amount, filled_amount,
			status, created_height, created_time, coalesce(created_tx_hash, '-'), coalesce(message_id, '-'),
			coalesce(closed_height::text, '-'), coalesce(closed_time::text, '-'))
		FROM explorer.orders ORDER BY order_id`)
}

func fillRows(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', height, seq, order_id, side, price, amount, maker, taker,
			coalesce(taker_tx_hash, '-'), time) FROM explorer.order_fills ORDER BY height, seq`)
}

// The backfill walks down, so an order's cancel is written before the
// order; the live path walks up. Both end with the same rows, and writing
// the heights again, in either mode, changes nothing.
func TestDexOrdersFromTheRecordedHeights(t *testing.T) {
	down := newSyncEnv(t)
	w := batchWriter(t, down.url)
	ctx := context.Background()
	for _, h := range slices.Backward(dexHeights) {
		require.NoError(t, w.Write(ctx, []*transform.Entities{transformed(t, down, h)}, writer.ModeInsert))
	}
	orders, fills := orderRows(t, down), fillRows(t, down)
	assert.Len(t, orders, 34)
	assert.Len(t, fills, 3)

	up := newSyncEnv(t)
	pool, err := pgxpool.New(ctx, up.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	live := writer.NewLiveWriter(pool)
	for _, h := range dexHeights {
		require.NoError(t, live.WriteBlock(ctx, transformed(t, up, h)))
	}
	assert.Equal(t, orders, orderRows(t, up), "the walking direction does not matter")
	assert.Equal(t, fills, fillRows(t, up))

	cancelled := queryStrings(t, down.db, `SELECT concat_ws(' ', count(*), min(closed_height), bool_and(closed_time = b.time)::text)
		FROM explorer.orders o JOIN explorer.blocks b ON b.height = 24998321
		WHERE o.created_height = 24998316 AND o.status = 'cancelled' GROUP BY b.time`)
	assert.Equal(t, []string{"11 24998321 true"}, cancelled, "every order saved at 24998316 is closed by its cancel")
	assert.Equal(t, []string{"4019206 filled 20230045"}, queryStrings(t, down.db,
		`SELECT concat_ws(' ', filled_amount, status, closed_height) FROM explorer.orders WHERE order_id = $1`,
		"000000000000000002157016"), "saved and filled in one EndBlock")
	assert.Equal(t, []string{"0"}, queryStrings(t, down.db, `SELECT count(*) FROM explorer.order_messages`),
		"no message ids before chain v8.2.0")

	for _, h := range dexHeights {
		require.NoError(t, live.WriteBlock(ctx, transformed(t, up, h)))
		require.NoError(t, w.Write(ctx, []*transform.Entities{transformed(t, down, h)}, writer.ModeUpdate))
	}
	assert.Equal(t, orders, orderRows(t, up), "a height written again moves no fill")
	assert.Equal(t, orders, orderRows(t, down), "a reindex changes nothing")
	assert.Equal(t, fills, fillRows(t, down))

	base := down.serveAPI(t)
	trader := "bze10kw8lpqd9emyxn94gkm038t4jj90ark4x8ls0d"
	open := into[dto.List[dto.Order]](t, fetch(t, base+"/accounts/"+trader+"/orders?status=open&limit=100"))
	require.Len(t, open.Items, 22, "the orders saved at 24998326 and 24999134 are still open")
	assert.Equal(t, int64(24999134), open.Items[0].CreatedHeight, "newest first")
	assert.Equal(t, int64(24998326), open.Items[21].CreatedHeight)
	for _, o := range open.Items {
		assert.NotNil(t, o.CreatedTxHash, "attributed by queue order before chain v8.2.0")
		assert.Nil(t, o.MessageID)
	}
	r := fetch(t, base+"/orders/2157016")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	order := into[dto.OrderDetail](t, r)
	assert.Equal(t, "filled", order.Status)
	assert.Equal(t, int64(1), order.FillCount)
}

const (
	dexH      = int64(26_000_000)
	dexOwner  = "bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk"
	dexMaker  = "bze15pqjgk4la0mfphwddce00d05n3th3u66n3ptcv"
	dexMarket = "factory/x/uhoney/ubze"
)

func oid(n int) string { return fmt.Sprintf("%024d", n) }

func txHash(n int) string { return fmt.Sprintf("%064X", n) }

// dexBlock is a chain v8.2.0 height with its transactions; no recorded
// height has message ids yet, so these are built by hand.
func dexBlock(h int64, txs ...transform.Transaction) *transform.Entities {
	at := time.Unix(1_800_000_000+h-dexH, 0).UTC()
	ents := &transform.Entities{Blocks: []transform.Block{{Height: h, Time: at, TxCount: len(txs), Hash: fmt.Sprintf("B%d", h)}}}
	for i, tx := range txs {
		tx.Height, tx.TxIndex, tx.Time, tx.Success = h, i, at, true
		tx.Signers = []string{dexOwner}
		ents.Transactions = append(ents.Transactions, tx)
	}
	return ents
}

func savedOrder(h int64, id int, owner, amount string, msg int, tx string) transform.Order {
	o := transform.Order{OrderID: oid(id), MarketID: dexMarket, Owner: owner, Side: "sell", Price: "2", Amount: amount,
		Height: h, Time: time.Unix(1_800_000_000+h-dexH, 0).UTC(), CreatedTxHash: tx}
	if msg > 0 {
		o.MessageID = oid(msg)
	}
	return o
}

func takenFill(h int64, seq, order int, amount string, msg int, tx string) transform.OrderFill {
	return transform.OrderFill{Height: h, Seq: seq, OrderID: oid(order), MarketID: dexMarket, Side: "sell", Price: "2",
		Amount: amount, Maker: dexMaker, Taker: dexOwner, TakerMessageID: oid(msg), TakerTxHash: tx,
		Time: time.Unix(1_800_000_000+h-dexH, 0).UTC()}
}

// dexHeightsByHand: at dexH-1 the maker rests orders 10 (100) and 11 (50),
// the owner order 12; at dexH the owner's create (message 1) fills 10
// completely and 11 partly and rests its remainder as order 20, a cancel
// (message 2) closes 12, a MsgFillOrders (messages 3 and 4) fills 11 again,
// and a create (message 5) has no outcome yet; at dexH+1 a message queued
// in an earlier block fills 11 once more.
func dexHeightsByHand() []*transform.Entities {
	before := dexBlock(dexH - 1)
	before.Orders = []transform.Order{
		savedOrder(dexH-1, 10, dexMaker, "100", 0, ""), savedOrder(dexH-1, 11, dexMaker, "50", 0, ""),
		savedOrder(dexH-1, 12, dexOwner, "40", 0, ""),
	}

	create, cancel, fills, queued := txHash(1), txHash(2), txHash(3), txHash(4)
	at := dexBlock(dexH,
		transform.Transaction{Hash: create, MsgCount: 1, MsgTypes: []string{"/bze.tradebin.MsgCreateOrder"}},
		transform.Transaction{Hash: cancel, MsgCount: 1, MsgTypes: []string{"/bze.tradebin.MsgCancelOrder"}},
		transform.Transaction{Hash: fills, MsgCount: 1, MsgTypes: []string{"/bze.tradebin.MsgFillOrders"}},
		transform.Transaction{Hash: queued, MsgCount: 1, MsgTypes: []string{"/bze.tradebin.MsgCreateOrder"}},
	)
	at.Messages = []transform.Message{{Height: dexH, TxIndex: 1, MsgIndex: 0, TypeURL: "/bze.tradebin.MsgCancelOrder",
		Events: []transform.Event{{Type: "bze.tradebin.OrderCancelMessageEvent",
			Attrs: map[string]any{"orderId": oid(12), "message_id": oid(2)}}}}}
	at.OrderMessages = []transform.OrderMessage{
		{Height: dexH, MessageID: oid(1), TxIndex: 0, TxHash: create, Kind: "create"},
		{Height: dexH, MessageID: oid(2), TxIndex: 1, TxHash: cancel, Kind: "cancel"},
		{Height: dexH, MessageID: oid(3), TxIndex: 2, TxHash: fills, Kind: "fill"},
		{Height: dexH, MessageID: oid(4), TxIndex: 2, TxHash: fills, Kind: "fill"},
		{Height: dexH, MessageID: oid(5), TxIndex: 3, TxHash: queued, Kind: "create"},
	}
	at.OrderFills = []transform.OrderFill{
		takenFill(dexH, 0, 10, "100", 1, create), takenFill(dexH, 1, 11, "20", 1, create),
		takenFill(dexH, 3, 11, "5", 3, fills), takenFill(dexH, 4, 11, "5", 4, fills),
	}
	at.Orders = []transform.Order{savedOrder(dexH, 20, dexOwner, "70", 1, create)}
	at.BlockEvents = []transform.BlockEvent{{Height: dexH, Seq: 2, Type: "bze.tradebin.OrderCanceledEvent",
		Attrs: map[string]any{"id": oid(12)}}}
	at.OrderCancels = []transform.OrderCancel{{OrderID: oid(12), Height: dexH, Time: at.Blocks[0].Time}}

	after := dexBlock(dexH + 1)
	after.OrderFills = []transform.OrderFill{takenFill(dexH+1, 0, 11, "10", 9, "")}
	return []*transform.Entities{before, at, after}
}

// Fills and cancels written before their order (the backfill direction)
// are read by the order's insert; a fill written after it moves it. The
// outcome of each transaction reads back on its page.
func TestDexOutcomesInEitherDirection(t *testing.T) {
	heights := dexHeightsByHand()
	ctx := context.Background()
	want := []string{
		oid(10) + " filled 100 " + fmt.Sprint(dexH),
		oid(11) + " open 40 -",
		oid(12) + " cancelled 0 " + fmt.Sprint(dexH),
		oid(20) + " open 0 -",
	}
	state := func(e *syncEnv) []string {
		return queryStrings(t, e.db, `SELECT concat_ws(' ', order_id, status, filled_amount, coalesce(closed_height::text, '-'))
			FROM explorer.orders ORDER BY order_id`)
	}

	down := newSyncEnv(t)
	w := batchWriter(t, down.url)
	for _, ents := range slices.Backward(heights) {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeInsert))
	}
	assert.Equal(t, want, state(down), "downward: fills and the cancel before their orders")
	assert.Equal(t, []string{"true"}, queryStrings(t, down.db, `SELECT (o.closed_time = b.time)::text
		FROM explorer.orders o JOIN explorer.blocks b ON b.height = o.closed_height WHERE o.order_id = $1`, oid(12)),
		"the cancel's block time")

	up := newSyncEnv(t)
	w = batchWriter(t, up.url)
	for _, ents := range heights {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeInsert))
	}
	assert.Equal(t, want, state(up), "upward")
	require.NoError(t, w.Write(ctx, heights, writer.ModeInsert))
	require.NoError(t, w.Write(ctx, heights, writer.ModeUpdate))
	assert.Equal(t, want, state(up), "written again: idempotent")

	one := newSyncEnv(t)
	require.NoError(t, batchWriter(t, one.url).Write(ctx, heights, writer.ModeInsert))
	assert.Equal(t, want, state(one), "all in one flush")

	base := down.serveAPI(t)
	tx := func(n int) (dto.Tx, response) {
		r := fetch(t, base+"/txs/"+txHash(n))
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		return into[dto.Tx](t, r), r
	}
	created, r := tx(1)
	assert.Equal(t, "public, max-age=60", r.header.Get("Cache-Control"))
	require.NotNil(t, created.Dex)
	require.Len(t, created.Dex.Placed, 1)
	assert.Equal(t, oid(20), created.Dex.Placed[0].OrderID)
	require.Len(t, created.Dex.Fills, 2, "fills of orders 10 and 11")
	assert.Equal(t, []string{oid(10), oid(11)}, []string{created.Dex.Fills[0].OrderID, created.Dex.Fills[1].OrderID})
	assert.False(t, created.Dex.Pending)

	cancelled, _ := tx(2)
	require.NotNil(t, cancelled.Dex)
	require.Len(t, cancelled.Dex.Cancelled, 1)
	assert.Equal(t, oid(12), cancelled.Dex.Cancelled[0].OrderID)
	assert.False(t, cancelled.Dex.Pending)

	filled, _ := tx(3)
	require.NotNil(t, filled.Dex)
	assert.Len(t, filled.Dex.Fills, 2)
	assert.Empty(t, filled.Dex.Placed, "a taker filled completely never rests")

	queued, r := tx(4)
	require.NotNil(t, queued.Dex)
	assert.True(t, queued.Dex.Pending, "no outcome indexed: pending, never failed")
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))

	order := into[dto.OrderDetail](t, fetch(t, base+"/orders/"+oid(11)))
	assert.Equal(t, "40", order.FilledAmount)
	assert.Equal(t, int64(4), order.FillCount)
	require.Len(t, order.Fills, 4)
	assert.Equal(t, dexH+1, order.Fills[0].Height, "newest first")
	assert.Nil(t, order.Fills[0].TakerTxHash, "queued in an earlier block: unattributed here")

	// The maker's fills come from the maker index, the owner's from the
	// taker index, three pages of two.
	var pages [][]dto.Fill
	url := base + "/accounts/" + dexMaker + "/fills?limit=2"
	for next := url; ; {
		page := into[dto.List[dto.Fill]](t, fetch(t, next))
		pages = append(pages, page.Items)
		if page.NextCursor == nil {
			break
		}
		next = url + "&cursor=" + *page.NextCursor
	}
	assert.Equal(t, []int{2, 2, 1}, []int{len(pages[0]), len(pages[1]), len(pages[2])})
	taker := into[dto.List[dto.Fill]](t, fetch(t, base+"/accounts/"+dexOwner+"/fills"))
	assert.Len(t, taker.Items, 5)

	owned := into[dto.List[dto.Order]](t, fetch(t, base+"/accounts/"+dexOwner+"/orders"))
	require.Len(t, owned.Items, 2)
	assert.Equal(t, oid(20), owned.Items[0].OrderID, "newest first")
	byStatus := into[dto.List[dto.Order]](t, fetch(t, base+"/accounts/"+dexOwner+"/orders?status=cancelled"))
	require.Len(t, byStatus.Items, 1)
	assert.Equal(t, oid(12), byStatus.Items[0].OrderID)

	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/orders/999").status)
	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/accounts/"+dexOwner+"/orders?status=partial").status)
}

// indexScans sums the scans of every partition of the partitioned index.
func indexScans(t *testing.T, db *sql.DB, index string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.QueryRow(`SELECT coalesce(sum(s.idx_scan), 0) FROM pg_inherits i
		JOIN pg_stat_user_indexes s ON s.indexrelid = i.inhrelid WHERE i.inhparent = $1::regclass`, index).Scan(&n))
	return n
}

// The orders insert finds each order's cancel through the cancel index. A
// plain index on the id made the planner walk the whole partition backwards
// on its primary key for the newest cancel, once per order, which took the
// backfill's single writer from milliseconds to seconds per batch.
func TestTheOrdersInsertFindsCancelsThroughTheirIndex(t *testing.T) {
	url := freshDatabase(t, true)
	migrateUp(t, url)
	db := connect(t, url)
	const events, orders = 40_000, 50
	_, err := db.Exec(`INSERT INTO explorer.block_events (height, seq, type, attrs)
		SELECT $1 + n / 20, n % 20,
		       CASE WHEN n % 2 = 0 THEN 'bze.tradebin.OrderCanceledEvent' ELSE 'bze.tradebin.OrderSavedEvent' END,
		       jsonb_build_object('id', lpad(n::text, 24, '0'))
		  FROM generate_series(1, $2) AS n`, dexH-events, events)
	require.NoError(t, err)
	_, err = db.Exec(`ANALYZE explorer.block_events`)
	require.NoError(t, err)

	at := dexBlock(dexH)
	for i := range orders {
		// Every other order was cancelled.
		at.Orders = append(at.Orders, savedOrder(dexH, 1+i, dexOwner, "10", 0, ""))
	}
	before := indexScans(t, db, "explorer.block_events_pkey")
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	err = writer.NewBatchWriter(pool).Write(context.Background(), []*transform.Entities{at}, writer.ModeInsert)
	pool.Close() // a closing connection reports its statistics at once
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return indexScans(t, db, "explorer.idx_block_events_order_cancel") > 0
	}, 10*time.Second, 50*time.Millisecond, "the cancel lookups use the cancel index")
	assert.Equal(t, before, indexScans(t, db, "explorer.block_events_pkey"), "no walk of a partition's primary key")
	assert.Equal(t, []string{fmt.Sprint(orders / 2)},
		queryStrings(t, db, `SELECT count(*) FROM explorer.orders WHERE status = 'cancelled'`))
}
