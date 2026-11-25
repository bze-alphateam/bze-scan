package transform_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// txAmended is an OrderAmendedEvent: the merged order rests under id with
// the total amount, replacing previous.
func txAmended(msgIndex, id, previous, added, total string) node.Event {
	return typed("bze.tradebin.OrderAmendedEvent", msgIndex, "id", id, "market_id", market, "order_type", "buy",
		"price", "2", "owner", trader, "added_amount", added, "amount", total, "previous_id", previous)
}

func txRefunded(msgIndex, orderID, amount, refund, reason string) node.Event {
	return typed("bze.tradebin.OrderMessageRefundedEvent", msgIndex, "order_id", orderID, "market_id", market,
		"order_type", "buy", "owner", trader, "amount", amount, "refund", refund, "reason", reason)
}

// mergeInput is a block of chain v8.3.0 transactions that merge: a create
// that merges its whole amount (tx 0), a create that fills a maker and then
// merges the rest (tx 1), a MsgFillOrders entry that is refunded
// (max_fills_reached, tx 2) and a failed create whose merge event is not
// read (tx 3).
func mergeInput(t *testing.T) transform.Input {
	in := baseInput()
	in.Block.Txs = []string{
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "40", Price: "2", MarketId: market}),
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "100", Price: "2", MarketId: market}),
		encodedTx(t, &tradebintypes.MsgFillOrders{Creator: trader, MarketId: market, OrderType: "buy",
			Orders: []*tradebintypes.FillOrderItem{{Price: "2", Amount: "9"}}}),
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "5", Price: "2", MarketId: market}),
	}
	in.Results.TxsResults = []node.TxResult{
		{Events: orderEvents(noise("0"), one(txAmended("0", msgID(1010), msgID(900), "40", "140")))},
		{Events: orderEvents(one(txExecuted("0", msgID(901), msgID(1011), "30", "2")),
			one(txAmended("0", msgID(1011), msgID(1010), "70", "210")))},
		{Events: orderEvents(one(txRefunded("0", msgID(1012), "9", "18ubze", "max_fills_reached")))},
		{Code: 11, Events: orderEvents(one(txAmended("0", msgID(1013), msgID(1011), "5", "215")))},
	}
	return in
}

// An OrderAmendedEvent is a new order under the new id, with what it
// replaced and added, and closes the replaced order as merged: not a cancel.
func TestAmendedOrdersMergeIntoTheirPredecessor(t *testing.T) {
	ents, err := realTransformer(t).Transform(mergeInput(t))
	require.NoError(t, err)
	hash := func(i int) string { return ents.Transactions[i].Hash }

	require.Len(t, ents.Orders, 2, "the failed transaction's event is not read")
	first, second := ents.Orders[0], ents.Orders[1]
	assert.Equal(t, transform.Order{OrderID: msgID(1010), MarketID: market, Owner: trader, Side: "buy", Price: "2",
		Amount: "140", Height: 10, Time: first.Time, CreatedTxHash: hash(0), MergedFrom: msgID(900), AddedAmount: "40"}, first,
		"the order rests under the new id with the merged total")
	assert.Equal(t, msgID(1011), second.OrderID)
	assert.Equal(t, "210", second.Amount)
	assert.Equal(t, msgID(1010), second.MergedFrom, "merging into an order that was itself merged")
	assert.Equal(t, "70", second.AddedAmount)
	assert.Equal(t, hash(1), second.CreatedTxHash)
	assert.Empty(t, first.MessageID)

	require.Len(t, ents.OrderClosures, 2)
	assert.Equal(t, transform.OrderClosure{OrderID: msgID(900), Height: 10, Time: first.Time, TxHash: hash(0),
		Reason: transform.OrderClosureMerged, MergedInto: msgID(1010)}, ents.OrderClosures[0])
	assert.Equal(t, msgID(1010), ents.OrderClosures[1].OrderID)
	assert.Equal(t, msgID(1011), ents.OrderClosures[1].MergedInto)
	assert.Empty(t, ents.OrderCancels, "a merge is not a cancel")

	require.Len(t, ents.OrderFills, 1, "a merge can follow fills of the same message")
	f := ents.OrderFills[0]
	assert.Equal(t, msgID(901), f.OrderID)
	assert.Equal(t, msgID(1011), f.TakerOrderID, "the taker is the merged order's id")
	assert.Equal(t, hash(1), f.TakerTxHash)
	assert.Equal(t, 0, *f.MsgEventIndex)
}

// A refund is read from the transaction's stored events when the page is
// built: the transformer writes no row for it, and keeps the event with the
// message.
func TestTxLevelRefundsStayInTheMessageEvents(t *testing.T) {
	ents, err := realTransformer(t).Transform(mergeInput(t))
	require.NoError(t, err)
	var refunded []transform.Message
	for _, m := range ents.Messages {
		for _, ev := range m.Events {
			if ev.Type == "bze.tradebin.OrderMessageRefundedEvent" {
				refunded = append(refunded, m)
				assert.Equal(t, msgID(1012), ev.Attrs["order_id"])
				assert.Equal(t, "max_fills_reached", ev.Attrs["reason"])
				assert.Equal(t, "18ubze", ev.Attrs["refund"])
			}
		}
	}
	require.Len(t, refunded, 1)
	assert.Equal(t, 2, refunded[0].TxIndex)
	assert.Empty(t, ents.OrderFills[1:], "the refund made no fill")
	assert.Empty(t, ents.OrderMessages, "nothing is queued")
}

// A reindex of the height writes what live indexing wrote.
func TestAmendedOrdersAreStable(t *testing.T) {
	first, err := realTransformer(t).Transform(mergeInput(t))
	require.NoError(t, err)
	second, err := realTransformer(t).Transform(mergeInput(t))
	require.NoError(t, err)
	assert.Equal(t, first.Orders, second.Orders)
	assert.Equal(t, first.OrderClosures, second.OrderClosures)
	assert.Equal(t, first.OrderFills, second.OrderFills)
}

// The signer's row of a create that merged carries the OrderAmendedEvent, so
// the activity list can say the order was added to; a create that did not
// merge has no details, as before.
func TestMergeIsOnTheSignersActivityRow(t *testing.T) {
	in := mergeInput(t)
	in.Block.Txs = append(in.Block.Txs, encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy",
		Amount: "3", Price: "2", MarketId: market}))
	in.Results.TxsResults = append(in.Results.TxsResults, node.TxResult{Events: orderEvents(one(txSaved("0", msgID(1014), "3")))})
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	rowOf := func(txIndex int) transform.Activity {
		for _, a := range ents.Activity {
			if a.Address == trader && a.TxIndex == txIndex && a.IsSigner {
				return a
			}
		}
		t.Fatalf("no signer row for tx %d", txIndex)
		return transform.Activity{}
	}
	for _, i := range []int{0, 1} {
		row := rowOf(i)
		assert.Equal(t, "dex_order", row.Kind)
		require.Len(t, row.Details, 1, "tx %d", i)
		assert.Equal(t, "bze.tradebin.OrderAmendedEvent", row.Details[0].Type)
		assert.Equal(t, trader, row.Details[0].Attrs["owner"])
		assert.Equal(t, msgID(1010+i), row.Details[0].Attrs["id"])
	}
	assert.Empty(t, rowOf(4).Details, "an order that rested on its own")
	assert.Equal(t, "dex_order", rowOf(4).Kind)
}
