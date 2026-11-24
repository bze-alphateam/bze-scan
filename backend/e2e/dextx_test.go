//go:build e2e

package e2e_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/trades"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Chain v8.3.0 runs the order book inside the message, so the outcomes are
// the transaction's own events: no message id, no queue, nothing pending.

func v8Event(typ, msgIndex string, kv ...string) node.Event {
	ev := node.Event{Type: typ}
	for i := 0; i < len(kv); i += 2 {
		ev.Attributes = append(ev.Attributes, node.Attribute{Key: kv[i], Value: `"` + kv[i+1] + `"`})
	}
	return withMsgIndex(ev, msgIndex)
}

func withMsgIndex(ev node.Event, msgIndex string) node.Event {
	ev.Attributes = append(ev.Attributes, node.Attribute{Key: "msg_index", Value: msgIndex})
	return ev
}

func v8Fill(maker, taker int, amount string) node.Event {
	return v8Event("bze.tradebin.OrderExecutedEvent", "0", "id", oid(maker), "market_id", dexMarket, "order_type", "sell",
		"amount", amount, "price", "2", "maker", dexMaker, "taker", dexOwner, "order_id", oid(taker))
}

func v8RawTx(t *testing.T, msgs ...sdk.Msg) string {
	t.Helper()
	anys := make([]*codectypes.Any, len(msgs))
	for i, m := range msgs {
		a, err := codectypes.NewAnyWithValue(m)
		require.NoError(t, err)
		anys[i] = a
	}
	body, err := (&sdktx.TxBody{Messages: anys}).Marshal()
	require.NoError(t, err)
	auth, err := (&sdktx.AuthInfo{Fee: &sdktx.Fee{Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 1))}}).Marshal()
	require.NoError(t, err)
	raw, err := (&sdktx.TxRaw{BodyBytes: body, AuthInfoBytes: auth}).Marshal()
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw)
}

// v830Block is the height dexH of a chain v8.3.0 book. Before it
// (dexH-1) the maker rests orders 10 (100), 11 (50) and 13 (30) and the
// owner order 12 (40). In it: tx 0 creates an order (id 20) that fills 10
// completely and 11 partly with the fee swap between the two fills, and
// rests its remainder (70); tx 1 cancels 12; tx 2 is a MsgFillOrders (taker
// order 21) filling 13 completely; tx 3 is a failed create.
func v830Block(t *testing.T) *transform.Entities {
	t.Helper()
	in := transform.Input{
		Block: &node.Block{Height: dexH, Time: time.Unix(1_800_000_000, 0).UTC(), Hash: "C0FFEE", Raw: json.RawMessage(`{}`),
			Txs: []string{
				v8RawTx(t, &tradebintypes.MsgCreateOrder{Creator: dexOwner, OrderType: "buy", Amount: "170", Price: "2", MarketId: dexMarket}),
				v8RawTx(t, &tradebintypes.MsgCancelOrder{Creator: dexOwner, MarketId: dexMarket, OrderId: oid(12), OrderType: "sell"}),
				v8RawTx(t, &tradebintypes.MsgFillOrders{Creator: dexOwner, MarketId: dexMarket, OrderType: "buy",
					Orders: []*tradebintypes.FillOrderItem{{Price: "2", Amount: "30"}}}),
				v8RawTx(t, &tradebintypes.MsgCreateOrder{Creator: dexOwner, OrderType: "buy", Amount: "9", Price: "2", MarketId: dexMarket}),
			}},
		Results: &node.BlockResults{Height: dexH, TxsResults: []node.TxResult{
			{Events: []node.Event{
				v8Fill(10, 20, "100"),
				swapEv("0", dexOwner, "3", swapHoney, "1", "ubze"),
				v8Fill(11, 20, "20"),
				v8Event("bze.tradebin.OrderSavedEvent", "0", "id", oid(20), "market_id", dexMarket, "order_type", "buy",
					"amount", "70", "price", "2", "owner", dexOwner, "order_id", oid(20)),
			}},
			{Events: []node.Event{v8Event("bze.tradebin.OrderCanceledEvent", "0", "id", oid(12), "market_id", dexMarket,
				"order_type", "sell", "amount", "40", "price", "2", "owner", dexOwner, "order_id", oid(12))}},
			{Events: []node.Event{v8Fill(13, 21, "30")}},
			{Code: 11},
		}},
		Commit: &node.Commit{Height: dexH},
	}
	ents, err := newTransformer(t).Transform(in)
	require.NoError(t, err)
	return ents
}

func v830Heights(t *testing.T) []*transform.Entities {
	t.Helper()
	before := dexBlock(dexH - 1)
	before.Orders = []transform.Order{
		savedOrder(dexH-1, 10, dexMaker, "100", 0, ""), savedOrder(dexH-1, 11, dexMaker, "50", 0, ""),
		savedOrder(dexH-1, 12, dexOwner, "40", 0, ""), savedOrder(dexH-1, 13, dexMaker, "30", 0, ""),
	}
	return []*transform.Entities{before, v830Block(t)}
}

func v830State(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', order_id, status, filled_amount, coalesce(closed_height::text, '-'),
			coalesce(created_tx_hash, '-') <> '-') FROM explorer.orders ORDER BY order_id`)
}

func v830Fills(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', height, seq, order_id, amount, taker_tx_hash, tx_index, msg_index,
			msg_event_index, taker_order_id, trade_id, coalesce(taker_message_id, '-')) FROM explorer.order_fills
		ORDER BY height, seq`)
}

// An order's cancel is a closure row written before the order when the
// backfill walks down; the live path walks up; a reindex changes nothing.
func TestDexTxLevelOutcomesInEitherDirection(t *testing.T) {
	heights := v830Heights(t)
	ctx := context.Background()
	want := []string{
		oid(10) + " filled 100 " + fmt.Sprint(dexH) + " false",
		oid(11) + " open 20 - false",
		oid(12) + " cancelled 0 " + fmt.Sprint(dexH) + " false",
		oid(13) + " filled 30 " + fmt.Sprint(dexH) + " false",
		oid(20) + " open 0 - true",
	}

	down := newSyncEnv(t)
	w := batchWriter(t, down.url)
	for _, ents := range slices.Backward(heights) {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeInsert))
	}
	assert.Equal(t, want, v830State(t, down), "downward: the closure and the fills before their orders")
	assert.Equal(t, []string{"true"}, queryStrings(t, down.db, `SELECT (o.closed_time = b.time)::text
		FROM explorer.orders o JOIN explorer.blocks b ON b.height = o.closed_height WHERE o.order_id = $1`, oid(12)))
	assert.Equal(t, []string{oid(12) + " " + fmt.Sprint(dexH) + " cancelled -"}, queryStrings(t, down.db,
		`SELECT concat_ws(' ', order_id, height, reason, coalesce(merged_into, '-')) FROM explorer.order_closures`))

	up := newSyncEnv(t)
	pool, err := pgxpool.New(ctx, up.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	live := writer.NewLiveWriter(pool)
	for _, ents := range heights {
		require.NoError(t, live.WriteBlock(ctx, ents))
	}
	assert.Equal(t, want, v830State(t, up), "upward")
	assert.Equal(t, v830Fills(t, down), v830Fills(t, up))

	fills := v830Fills(t, up)
	require.Len(t, fills, 3)
	id := func(n int64) int64 { return dexH*transform.TradeIDsPerBlock + n }
	tx0 := queryStrings(t, up.db, `SELECT hash FROM explorer.transactions WHERE height = $1 AND tx_index = 0`, dexH)[0]
	tx2 := queryStrings(t, up.db, `SELECT hash FROM explorer.transactions WHERE height = $1 AND tx_index = 2`, dexH)[0]
	assert.Equal(t, []string{
		fmt.Sprintf("%d 1000000 %s 100 %s 0 0 0 %s %d -", dexH, oid(10), tx0, oid(20), id(1)),
		fmt.Sprintf("%d 1000001 %s 20 %s 0 0 2 %s %d -", dexH, oid(11), tx0, oid(20), id(3)),
		fmt.Sprintf("%d 1000002 %s 30 %s 2 0 0 %s %d -", dexH, oid(13), tx2, oid(21), id(4)),
	}, fills, "the fee swap (trade 2) sits at position 1, between the fills")
	swaps := swapRows(t, up)
	require.Len(t, swaps, 1)
	assert.Contains(t, swaps[0], fmt.Sprintf(" %d ", id(2)))

	// Writing again, in either mode, changes nothing: a reindex of the
	// height leaves the same rows.
	for range 2 {
		require.NoError(t, live.WriteBlock(ctx, v830Block(t)))
		require.NoError(t, w.Write(ctx, []*transform.Entities{v830Block(t)}, writer.ModeUpdate))
	}
	assert.Equal(t, want, v830State(t, up))
	assert.Equal(t, want, v830State(t, down))
	assert.Equal(t, fills, v830Fills(t, up))
	assert.Equal(t, fills, v830Fills(t, down))
	assert.Equal(t, swaps, swapRows(t, up))

	// The stored block renumbers to the transformer's ids.
	for _, q := range []string{`DELETE FROM explorer.swaps`, `UPDATE explorer.order_fills SET trade_id = NULL`} {
		_, err := e2eExec(up, q)
		require.NoError(t, err)
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = trades.Range(ctx, tx, dexH, dexH+1, candles.Default)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	assert.Equal(t, fills, v830Fills(t, up), "trades.Range gives the transformer's trade ids")
	assert.Equal(t, swaps, swapRows(t, up))
}

func e2eExec(e *syncEnv, q string) (any, error) { return e.db.Exec(q) }

func TestDexTxLevelOutcomesOverTheAPI(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	w := batchWriter(t, e.url)
	for _, ents := range v830Heights(t) {
		require.NoError(t, w.Write(ctx, []*transform.Entities{ents}, writer.ModeInsert))
	}
	base := e.serveAPI(t)
	hash := func(i int) string {
		return queryStrings(t, e.db, `SELECT hash FROM explorer.transactions WHERE height = $1 AND tx_index = $2`, dexH, i)[0]
	}
	tx := func(i int) dto.Tx {
		r := fetch(t, base+"/txs/"+hash(i))
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		return into[dto.Tx](t, r)
	}

	created := tx(0)
	require.NotNil(t, created.Dex)
	require.Len(t, created.Dex.Placed, 1)
	assert.Equal(t, oid(20), created.Dex.Placed[0].OrderID)
	assert.Nil(t, created.Dex.Placed[0].MessageID)
	require.Len(t, created.Dex.Fills, 2)
	assert.Equal(t, []string{oid(10), oid(11)}, []string{created.Dex.Fills[0].OrderID, created.Dex.Fills[1].OrderID})
	require.NotNil(t, created.Dex.Fills[0].TakerOrderID)
	assert.Equal(t, oid(20), *created.Dex.Fills[0].TakerOrderID)
	assert.False(t, created.Dex.Pending, "nothing is queued")

	cancelled := tx(1)
	require.NotNil(t, cancelled.Dex)
	require.Len(t, cancelled.Dex.Cancelled, 1)
	assert.Equal(t, oid(12), cancelled.Dex.Cancelled[0].OrderID)
	assert.False(t, cancelled.Dex.Pending)

	filled := tx(2)
	require.NotNil(t, filled.Dex)
	require.Len(t, filled.Dex.Fills, 1)
	assert.Empty(t, filled.Dex.Placed, "a taker filled completely never rests")
	assert.False(t, filled.Dex.Pending)

	order := into[dto.OrderDetail](t, fetch(t, base+"/orders/"+oid(11)))
	assert.Equal(t, "20", order.FilledAmount)
	assert.Equal(t, int64(1), order.FillCount)
	require.Len(t, order.Fills, 1)
	require.NotNil(t, order.Fills[0].TakerTxHash)
	assert.Equal(t, hash(0), *order.Fills[0].TakerTxHash)
	assert.Equal(t, "cancelled", into[dto.OrderDetail](t, fetch(t, base+"/orders/"+oid(12))).Status)

	// The maker is named by the taker's transactions: its rows say its
	// resting order was filled, with the fills as details.
	page := into[dto.List[dto.Activity]](t, fetch(t, base+"/accounts/"+dexMaker+"/activity"))
	require.Len(t, page.Items, 2, "tx 2 and tx 0; the cancel names the owner only")
	for _, a := range page.Items {
		assert.Equal(t, "dex_maker_fill", a.Kind)
		assert.Equal(t, "dex", a.Category)
		assert.False(t, a.IsSigner)
		require.NotNil(t, a.Counterparty)
		assert.Equal(t, dexOwner, a.Counterparty.Address)
	}
	var details []struct {
		Type  string         `json:"type"`
		Attrs map[string]any `json:"attrs"`
	}
	require.NoError(t, json.Unmarshal(page.Items[1].Details, &details))
	require.Len(t, details, 2, "both fills of tx 0")
	assert.Equal(t, oid(10), details[0].Attrs["id"])
	signer := into[dto.List[dto.Activity]](t, fetch(t, base+"/accounts/"+dexOwner+"/activity"))
	assert.Equal(t, "dex_order", signer.Items[len(signer.Items)-1].Kind, "the signer keeps the message's kind")
}

// An order placed by a transaction's event is on the book the index
// answers: the order of tx 0, partly filled (70 left of the 170 asked).
func TestOpenOrdersOfATxLevelOrder(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	live := writer.NewLiveWriter(pool, writer.AttributeOrders)
	for _, ents := range v830Heights(t) {
		require.NoError(t, live.WriteBlock(ctx, ents))
	}
	withHoneyMarket(t, e.grpc)
	noOrdersOnTheNode(e.grpc, dexMaker, dexOwner)

	base := e.serveOpenOrders(t, "beezee-1", e.node.URL, map[string]int64{"beezee-1": dexH - 1})
	waitForIndex(t, base)
	got := openOrdersOf(t, base, dexOwner)
	assert.Equal(t, "index", got.Source)
	assert.Equal(t, 1, got.OrdersCount, "order 20 rests; order 12 was cancelled")
	// The buy order locks the quote; the maker's order 11 keeps 30 of 50.
	maker := openOrdersOf(t, base, dexMaker)
	assert.Equal(t, "index", maker.Source)
	assert.Equal(t, 1, maker.OrdersCount)
}
