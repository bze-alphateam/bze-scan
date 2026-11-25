//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/orders"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// v8Height builds the entities of a chain v8.3.0 height from its
// transactions and their results.
func v8Height(t *testing.T, h int64, msgs []sdk.Msg, results []node.TxResult) *transform.Entities {
	t.Helper()
	txs := make([]string, len(msgs))
	for i, m := range msgs {
		txs[i] = v8RawTx(t, m)
	}
	in := transform.Input{
		Block: &node.Block{Height: h, Time: time.Unix(1_800_000_000+h-dexH, 0).UTC(), Hash: fmt.Sprintf("C0FFEE%d", h),
			Raw: json.RawMessage(`{}`), Txs: txs},
		Results: &node.BlockResults{Height: h, TxsResults: results},
		Commit:  &node.Commit{Height: h},
	}
	ents, err := newTransformer(t).Transform(in)
	require.NoError(t, err)
	return ents
}

func create(owner, amount string) sdk.Msg {
	return &tradebintypes.MsgCreateOrder{Creator: owner, OrderType: "buy", Amount: amount, Price: "2", MarketId: dexMarket}
}

func amended(id, previous, added, total string) node.Event {
	return v8Event("bze.tradebin.OrderAmendedEvent", "0", "id", id, "market_id", dexMarket, "order_type", "buy",
		"price", "2", "owner", dexOwner, "added_amount", added, "amount", total, "previous_id", previous)
}

func refunded(orderID, amount, refund, reason string) node.Event {
	return v8Event("bze.tradebin.OrderMessageRefundedEvent", "0", "order_id", orderID, "market_id", dexMarket,
		"order_type", "buy", "owner", dexOwner, "amount", amount, "refund", refund, "reason", reason)
}

// mergeHeights is the book of a chain v8.3.0 merge. At dexH-1 the owner
// rests order 30 (60) from before the upgrade, block-level history, and the
// maker order 10 (100). At dexH tx 0 creates 40 more at the same price: it is
// merged into 30, so order 40 rests with 100. At dexH+1 tx 0 creates 50 more
// that fills 20 of the maker's order 10 and merges its remaining 25 into 40:
// order 41 rests with 125.
func mergeHeights(t *testing.T) []*transform.Entities {
	t.Helper()
	before := dexBlock(dexH - 1)
	old := savedOrder(dexH-1, 30, dexOwner, "60", 0, "")
	old.Side = "buy"
	before.Orders = []transform.Order{old, savedOrder(dexH-1, 10, dexMaker, "100", 0, "")}
	return []*transform.Entities{
		before,
		v8Height(t, dexH, []sdk.Msg{create(dexOwner, "40")}, []node.TxResult{
			{Events: []node.Event{amended(oid(40), oid(30), "40", "100")}}}),
		v8Height(t, dexH+1, []sdk.Msg{create(dexOwner, "50")}, []node.TxResult{
			{Events: []node.Event{v8Fill(10, 41, "20"), amended(oid(41), oid(40), "25", "125")}}}),
	}
}

func mergeState(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', order_id, status, amount, filled_amount, coalesce(merged_from, '-'),
			coalesce(added_amount::text, '-'), coalesce(closed_height::text, '-'), coalesce(to_char(closed_time AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS'), '-'),
			(created_tx_hash IS NOT NULL)::text) FROM explorer.orders ORDER BY order_id`)
}

func closureState(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', order_id, height, reason, coalesce(merged_into, '-'))
		FROM explorer.order_closures ORDER BY order_id`)
}

// A merge closes the replaced order as merged, whichever order the heights
// are written in: the live path walks up; the backfill walks down, so the
// merge is written before the order it replaced, and that order is inserted
// as merged. A reindex changes nothing and restores what a previous build
// did not write.
func TestMergedOrdersInEitherDirection(t *testing.T) {
	heights := mergeHeights(t)
	ctx := context.Background()
	closedAt := func(h int64) string { return time.Unix(1_800_000_000+h-dexH, 0).UTC().Format("2006-01-02 15:04:05") }
	want := []string{
		oid(10) + " open 100 20 - - - - false",
		oid(30) + " merged 60 0 - - " + fmt.Sprint(dexH) + " " + closedAt(dexH) + " false",
		oid(40) + " merged 100 0 " + oid(30) + " 40 " + fmt.Sprint(dexH+1) + " " + closedAt(dexH+1) + " true",
		oid(41) + " open 125 0 " + oid(40) + " 25 - - true",
	}
	wantClosures := []string{
		oid(30) + " " + fmt.Sprint(dexH) + " merged " + oid(40),
		oid(40) + " " + fmt.Sprint(dexH+1) + " merged " + oid(41),
	}

	down := newSyncEnv(t)
	w := batchWriter(t, down.url)
	for _, ents := range slices.Backward(heights) {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeInsert))
	}
	assert.Equal(t, wantClosures, closureState(t, down))
	got := mergeState(t, down)
	require.Len(t, got, 4)
	assert.Equal(t, want[1], got[1], "order 30 was inserted last, as merged")
	assert.Equal(t, want[2], got[2], "order 40 was inserted with its closure already stored")

	up := newSyncEnv(t)
	pool, err := pgxpool.New(ctx, up.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	live := writer.NewLiveWriter(pool)
	for _, ents := range heights {
		require.NoError(t, live.WriteBlock(ctx, ents))
	}
	assert.Equal(t, want, mergeState(t, up), "upward")
	assert.Equal(t, want, mergeState(t, down), "downward gives the same rows")
	assert.Equal(t, wantClosures, closureState(t, up))

	// The merged order's fills start at zero; its fill sits on the maker's.
	assert.Equal(t, []string{"20"}, queryStrings(t, up.db, `SELECT filled_amount::text FROM explorer.orders WHERE order_id = $1`, oid(10)))

	// Writing again, in either mode, changes nothing.
	for range 2 {
		for _, ents := range heights {
			require.NoError(t, live.WriteBlock(ctx, ents))
			require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeUpdate))
		}
	}
	assert.Equal(t, want, mergeState(t, up))
	assert.Equal(t, want, mergeState(t, down))
	assert.Equal(t, wantClosures, closureState(t, down))

	// Rows a previous build wrote lack the merge columns, and a merged order
	// is still open there: a reindex fills them in.
	// The hourly sync of that build had closed the orders the node no longer
	// listed as filled, at a later height.
	for _, q := range []string{
		`UPDATE explorer.orders SET merged_from = NULL, added_amount = NULL`,
		`UPDATE explorer.orders SET status = 'filled', closed_height = ` + fmt.Sprint(dexH+9) + `, closed_time = now()
			WHERE order_id IN ('` + oid(30) + `', '` + oid(40) + `')`,
		`DELETE FROM explorer.order_closures`,
	} {
		_, err := e2eExec(down, q)
		require.NoError(t, err)
	}
	for _, ents := range heights {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeUpdate))
	}
	assert.Equal(t, want, mergeState(t, down), "the reindex turns the sync's filled into merged, at the merge height")
	assert.Equal(t, wantClosures, closureState(t, down))
}

// Activity rows written by the build before this one have no details: a
// reindex of the height adds the amend to the signer's row.
func TestReindexAddsTheAmendToTheSignersActivityRow(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	w := batchWriter(t, e.url)
	heights := mergeHeights(t)
	for _, ents := range heights {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeInsert))
	}
	count := func() []string {
		return queryStrings(t, e.db, `SELECT count(*)::text FROM explorer.account_activity
			WHERE height = $1 AND kind = 'dex_order' AND jsonb_array_length(details) = 1`, dexH)
	}
	assert.Equal(t, []string{"1"}, count())
	_, err := e.db.Exec(`UPDATE explorer.account_activity SET details = '[]' WHERE height = $1`, dexH)
	require.NoError(t, err)
	assert.Equal(t, []string{"0"}, count(), "a row as the previous build wrote it")
	require.NoError(t, w.Write(ctx, []*transform.Entities{heights[1]}, writer.ModeUpdate))
	assert.Equal(t, []string{"1"}, count())
}

func TestMergedOrdersOverTheAPI(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	w := batchWriter(t, e.url)
	for _, ents := range mergeHeights(t) {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeInsert))
	}
	base := e.serveAPI(t)
	order := func(n int) dto.OrderDetail {
		r := fetch(t, base+"/orders/"+oid(n))
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		return into[dto.OrderDetail](t, r)
	}
	ptr := func(s *string) string {
		if s == nil {
			return "-"
		}
		return *s
	}

	replaced := order(30)
	assert.Equal(t, "merged", replaced.Status)
	assert.Equal(t, oid(40), ptr(replaced.MergedInto))
	assert.Equal(t, "-", ptr(replaced.MergedFrom))
	assert.Equal(t, "-", ptr(replaced.AddedAmount))
	require.NotNil(t, replaced.ClosedHeight)
	assert.Equal(t, dexH, *replaced.ClosedHeight)

	middle := order(40)
	assert.Equal(t, "merged", middle.Status, "merged again by the next create")
	assert.Equal(t, oid(30), ptr(middle.MergedFrom))
	assert.Equal(t, "40", ptr(middle.AddedAmount))
	assert.Equal(t, oid(41), ptr(middle.MergedInto))
	assert.Equal(t, "100", middle.Amount, "the merged total at its creation")

	resting := order(41)
	assert.Equal(t, "open", resting.Status)
	assert.Equal(t, oid(40), ptr(resting.MergedFrom))
	assert.Equal(t, "25", ptr(resting.AddedAmount))
	assert.Equal(t, "125", resting.Amount)
	assert.Equal(t, "0", resting.FilledAmount, "its fills start at zero")
	assert.Equal(t, "-", ptr(resting.MergedInto))

	maker := order(10)
	assert.Equal(t, "-", ptr(maker.MergedFrom))
	assert.Equal(t, "-", ptr(maker.MergedInto), "an ordinary order has no merge")
	assert.Equal(t, "20", maker.FilledAmount)

	// The owner's list filters by the new status.
	merged := into[dto.List[dto.Order]](t, fetch(t, base+"/accounts/"+dexOwner+"/orders?status=merged"))
	require.Len(t, merged.Items, 2)
	assert.Equal(t, []string{oid(40), oid(30)}, []string{merged.Items[0].OrderID, merged.Items[1].OrderID}, "newest first")
	open := into[dto.List[dto.Order]](t, fetch(t, base+"/accounts/"+dexOwner+"/orders?status=open"))
	require.Len(t, open.Items, 1)
	assert.Equal(t, oid(41), open.Items[0].OrderID)

	// The transaction shows the fill and the merged order together.
	hash := queryStrings(t, e.db, `SELECT hash FROM explorer.transactions WHERE height = $1`, dexH+1)[0]
	tx := into[dto.Tx](t, fetch(t, base+"/txs/"+hash))
	require.NotNil(t, tx.Dex)
	require.Len(t, tx.Dex.Placed, 1)
	assert.Equal(t, oid(41), tx.Dex.Placed[0].OrderID)
	assert.Equal(t, oid(40), ptr(tx.Dex.Placed[0].MergedFrom))
	require.Len(t, tx.Dex.Fills, 1)
	assert.Equal(t, oid(10), tx.Dex.Fills[0].OrderID)
	assert.Equal(t, oid(41), *tx.Dex.Fills[0].TakerOrderID, "the taker is the merged order's id")
	assert.Empty(t, tx.Dex.Cancelled, "a merge is not a cancel")
	assert.False(t, tx.Dex.Pending)

	// The owner's activity row of the create that merged carries the amend, so
	// the list can say the order was added to.
	amendRows := func(items []dto.Activity) []string {
		var out []string
		for _, a := range items {
			var details []struct {
				Type  string         `json:"type"`
				Attrs map[string]any `json:"attrs"`
			}
			if a.Height == dexH && a.IsSigner && len(a.Details) > 0 {
				require.NoError(t, json.Unmarshal(a.Details, &details))
			}
			for _, d := range details {
				out = append(out, fmt.Sprint(a.Kind, " ", d.Type, " ", d.Attrs["id"], " ", d.Attrs["previous_id"]))
			}
		}
		return out
	}
	activity := into[dto.List[dto.Activity]](t, fetch(t, base+"/accounts/"+dexOwner+"/activity"))
	assert.Equal(t, []string{"dex_order bze.tradebin.OrderAmendedEvent " + oid(40) + " " + oid(30)}, amendRows(activity.Items))

	// A cancel of the id that no longer exists was refused by the chain: the
	// replaced order is never listed as cancelled.
	assert.Equal(t, []string{"0"}, queryStrings(t, e.db, `SELECT count(*)::text FROM explorer.orders WHERE status = 'cancelled'`))
}

// The hourly orders sync lists the open orders and closes the ones the node
// no longer has as filled: a merged order is not open, and is never touched.
func TestOrdersSyncLeavesMergedOrdersAlone(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	w := batchWriter(t, e.url)
	for _, ents := range mergeHeights(t) {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeInsert))
	}
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := orders.NewPGStore(pool)

	open, err := store.Open(ctx, dexH+1)
	require.NoError(t, err)
	var ids []string
	for _, o := range open {
		ids = append(ids, o.OrderID)
	}
	assert.Equal(t, []string{oid(10), oid(41)}, ids, "30 and 40 are merged, not open")

	before := mergeState(t, e)
	n, err := store.Close(ctx, []string{oid(30), oid(40)}, orders.Position{Height: dexH + 5, Time: time.Unix(1_800_000_100, 0).UTC()})
	require.NoError(t, err)
	assert.Zero(t, n, "nothing to close")
	assert.Equal(t, before, mergeState(t, e), "the merged rows keep status, closed height and time")
}

// A transaction's own refunds (chain v8.3.0) have the taker order's id, no
// message id and no pending flag: a create stopped by the fill cap, and one
// MsgFillOrders entry stopped the same way next to a dust refund.
func TestDexTxLevelRefunds(t *testing.T) {
	heights := []*transform.Entities{
		dexBlock(dexH - 1),
		v8Height(t, dexH, []sdk.Msg{
			create(dexOwner, "9000"),
			&tradebintypes.MsgFillOrders{Creator: dexOwner, MarketId: dexMarket, OrderType: "buy",
				Orders: []*tradebintypes.FillOrderItem{{Price: "2", Amount: "70"}, {Price: "3", Amount: "1"}}},
		}, []node.TxResult{
			{Events: []node.Event{v8Fill(10, 20, "500"), refunded(oid(20), "8500", "17000ubze", "max_fills_reached")}},
			{Events: []node.Event{v8Fill(11, 21, "50"),
				refunded(oid(21), "20", "40ubze", "max_fills_reached"),
				refunded(oid(22), "1", "", "below_min_amount")}},
		}),
	}
	heights[0].Orders = []transform.Order{savedOrder(dexH-1, 10, dexMaker, "500", 0, ""), savedOrder(dexH-1, 11, dexMaker, "50", 0, "")}

	e := newSyncEnv(t)
	w := batchWriter(t, e.url)
	ctx := context.Background()
	for _, ents := range heights {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeInsert))
	}
	require.NoError(t, w.Write(ctx, []*transform.Entities{heights[1]}, writer.ModeUpdate), "a reindex")
	base := e.serveAPI(t)
	tx := func(i int) (dto.TxDex, response) {
		hash := queryStrings(t, e.db, `SELECT hash FROM explorer.transactions WHERE height = $1 AND tx_index = $2`, dexH, i)[0]
		r := fetch(t, base+"/txs/"+hash)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		d := into[dto.Tx](t, r).Dex
		require.NotNil(t, d)
		return *d, r
	}

	d, r := tx(0)
	assert.Equal(t, []dto.Refund{{Height: dexH, OrderID: oid(20), MarketID: dexMarket, OrderType: "buy", Amount: "8500",
		Refund: &dto.Coin{Denom: "ubze", Amount: "17000"}, Reason: "max_fills_reached"}}, d.Refunds)
	assert.False(t, d.Pending)
	assert.Empty(t, d.Placed, "a remainder that was refunded never rests")
	require.Len(t, d.Fills, 1)
	assert.Equal(t, "public, max-age=60", r.header.Get("Cache-Control"))
	assert.NotContains(t, string(r.body), `"message_id":""`)
	assert.Contains(t, string(r.body), `"skipped_cancels":[],"failed":[]`)

	d, _ = tx(1)
	require.Len(t, d.Refunds, 2, "the entries' refunds in event order")
	assert.Equal(t, oid(21), d.Refunds[0].OrderID)
	assert.Equal(t, "max_fills_reached", d.Refunds[0].Reason)
	assert.Equal(t, "", d.Refunds[0].MessageID)
	assert.Equal(t, oid(22), d.Refunds[1].OrderID)
	assert.Equal(t, "below_min_amount", d.Refunds[1].Reason)
	assert.Nil(t, d.Refunds[1].Refund, "rounded to zero: only dust was kept")
	assert.False(t, d.Pending)
}

// The v8.3.0 upgrade block sends back the messages a v8.2.x transaction
// still had queued, with a block-level refund named by the old queue message
// id: the transactions three and one blocks earlier each get theirs, and
// stop being pending; a third one, queued again after the upgrade's queue
// cleared (the id was taken by a later message), is not claimed by it.
func TestUpgradeBlockRefundsLinkToTheQueuedTransactions(t *testing.T) {
	const upgrade = dexH
	queuedA, queuedB := txHash(31), txHash(32)
	a := dexBlock(upgrade-3, transform.Transaction{Hash: queuedA, MsgCount: 1, MsgTypes: []string{"/bze.tradebin.MsgCreateOrder"}})
	a.OrderMessages = []transform.OrderMessage{{Height: upgrade - 3, MessageID: oid(1), TxIndex: 0, TxHash: queuedA, Kind: "create"}}
	b := dexBlock(upgrade-1, transform.Transaction{Hash: queuedB, MsgCount: 1, MsgTypes: []string{"/bze.tradebin.MsgFillOrders"}})
	b.OrderMessages = []transform.OrderMessage{{Height: upgrade - 1, MessageID: oid(2), TxIndex: 0, TxHash: queuedB, Kind: "fill"}}
	block := dexBlock(upgrade)
	block.BlockEvents = []transform.BlockEvent{
		outcomeEvent(upgrade, 0, "OrderMessageRefundedEvent", map[string]any{"order_id": oid(1), "market_id": dexMarket,
			"order_type": "buy", "owner": dexOwner, "amount": "100", "refund": "200ubze", "reason": "upgrade"}),
		outcomeEvent(upgrade, 1, "OrderMessageRefundedEvent", map[string]any{"order_id": oid(2), "market_id": dexMarket,
			"order_type": "sell", "owner": dexOwner, "amount": "7", "refund": "", "reason": "upgrade"}),
	}
	// After the upgrade a new, unrelated message takes id 1 again.
	reused := txHash(33)
	after := dexBlock(upgrade+1, transform.Transaction{Hash: reused, MsgCount: 1, MsgTypes: []string{"/bze.tradebin.MsgCreateOrder"}})
	after.OrderMessages = []transform.OrderMessage{{Height: upgrade + 1, MessageID: oid(1), TxIndex: 0, TxHash: reused, Kind: "create"}}

	e := newSyncEnv(t)
	ctx := context.Background()
	w := batchWriter(t, e.url)
	base := e.serveAPI(t)
	tx := func(hash string) *dto.TxDex {
		r := fetch(t, base+"/txs/"+hash)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		return into[dto.Tx](t, r).Dex
	}

	require.NoError(t, w.Write(ctx, []*transform.Entities{a, b}, writer.ModeInsert))
	assert.True(t, tx(queuedA).Pending, "queued, no outcome yet")
	assert.True(t, tx(queuedB).Pending)

	require.NoError(t, w.Write(ctx, []*transform.Entities{block, after}, writer.ModeInsert))
	require.NoError(t, w.Write(ctx, []*transform.Entities{a, b, block, after}, writer.ModeUpdate), "a reindex")

	first := tx(queuedA)
	require.NotNil(t, first)
	assert.False(t, first.Pending, "the upgrade sent it back")
	assert.Equal(t, []dto.Refund{{Height: upgrade, MessageID: oid(1), OrderID: oid(1), MarketID: dexMarket,
		OrderType: "buy", Amount: "100", Refund: &dto.Coin{Denom: "ubze", Amount: "200"}, Reason: "upgrade"}}, first.Refunds)

	second := tx(queuedB)
	require.NotNil(t, second)
	assert.False(t, second.Pending)
	require.Len(t, second.Refunds, 1)
	assert.Equal(t, "upgrade", second.Refunds[0].Reason)
	assert.Equal(t, oid(2), second.Refunds[0].OrderID)
	assert.Nil(t, second.Refunds[0].Refund, "rounded to zero: dust")

	later := tx(reused)
	require.NotNil(t, later)
	assert.True(t, later.Pending, "the refund was before this message took id 1")
	assert.Empty(t, later.Refunds)
}
