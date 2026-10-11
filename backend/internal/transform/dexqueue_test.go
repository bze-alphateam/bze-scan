package transform_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// Before chain v8.2.0 no order event carries a message id: the outcomes
// are matched to the same block's transactions by queue order.
func TestFixtureOutcomesAttributedByQueueOrder(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	transformed := func(h int64) *transform.Entities {
		ents, err := tr.Transform(fetchInput(t, n, h))
		require.NoError(t, err)
		return ents
	}
	placedBy := func(ents *transform.Entities) map[string]string {
		out := map[string]string{}
		for _, o := range ents.Orders {
			out[o.OrderID] = o.CreatedTxHash
		}
		return out
	}

	t.Run("one owner's buy rests, then their sell fills an older order and the buy (v8.0.x)", func(t *testing.T) {
		ents := transformed(20300028)
		require.Len(t, ents.Transactions, 1)
		tx := ents.Transactions[0].Hash
		assert.Equal(t, map[string]string{"000000000000000002161248": tx}, placedBy(ents))
		require.Len(t, ents.OrderFills, 2)
		for _, f := range ents.OrderFills {
			assert.Equal(t, tx, f.TakerTxHash)
		}
		assert.Empty(t, ents.OrderMessages, "no message ids before chain v8.2.0")
	})

	t.Run("eleven orders of one owner in two transactions, each to its own; a third ran out of gas", func(t *testing.T) {
		ents := transformed(24998316)
		require.Len(t, ents.Transactions, 3)
		require.False(t, ents.Transactions[2].Success)
		sells, buys := ents.Transactions[0].Hash, ents.Transactions[1].Hash
		require.Len(t, ents.Orders, 11)
		for _, o := range ents.Orders {
			want := buys
			if o.Side == "sell" {
				want = sells
			}
			assert.Equal(t, want, o.CreatedTxHash, o.OrderID)
			assert.Empty(t, o.MessageID)
		}
	})

	t.Run("a MsgFillOrders body takes five fills (v7.2)", func(t *testing.T) {
		ents := transformed(20234375)
		require.Len(t, ents.OrderFills, 5)
		var fillTx string
		for _, tx := range ents.Transactions {
			if len(tx.MsgTypes) == 1 && tx.MsgTypes[0] == "/bze.tradebin.MsgFillOrders" {
				fillTx = tx.Hash
			}
		}
		require.NotEmpty(t, fillTx)
		for _, f := range ents.OrderFills {
			assert.Equal(t, fillTx, f.TakerTxHash)
		}
	})

	t.Run("a sell rests and a buy of the same block fills it (v7.2)", func(t *testing.T) {
		ents := transformed(20230045)
		require.Len(t, ents.Orders, 1)
		require.Len(t, ents.OrderFills, 2)
		assert.NotEmpty(t, ents.Orders[0].CreatedTxHash)
		for _, f := range ents.OrderFills {
			assert.Equal(t, ents.Orders[0].CreatedTxHash, f.TakerTxHash, "one transaction placed both orders")
		}
	})
}

const otherMarket = "ubze/factory/x/ucoin"

// createEvent is a pre-v8.2.0 OrderCreateMessageEvent: no message id.
func createEvent(owner, mkt, side, amount, price string) node.Event {
	return typed("bze.tradebin.OrderCreateMessageEvent", "0", "creator", owner, "market_id", mkt,
		"order_type", side, "amount", amount, "price", price)
}

func createTx(t *testing.T, owner, mkt, side, amount, price string) (string, node.TxResult) {
	return encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: owner, OrderType: side, Amount: amount, Price: price,
			MarketId: mkt}),
		node.TxResult{Events: []node.Event{createEvent(owner, mkt, side, amount, price)}}
}

// executed and saved are pre-v8.2.0 outcome events; an empty taker or
// owner is a chain v7.0 event, which names none.
func executed(order, mkt, restingSide, amount, price, taker string) node.Event {
	kv := []string{"id", order, "market_id", mkt, "order_type", restingSide, "amount", amount, "price", price,
		"maker", maker1}
	if taker != "" {
		kv = append(kv, "taker", taker)
	}
	return typed("bze.tradebin.OrderExecutedEvent", "", kv...)
}

func saved(order, mkt, side, amount, price, owner string) node.Event {
	kv := []string{"id", order, "market_id", mkt, "order_type", side, "amount", amount, "price", price}
	if owner != "" {
		kv = append(kv, "owner", owner)
	}
	return typed("bze.tradebin.OrderSavedEvent", "", kv...)
}

// queueInput is a pre-v8.2.0 block of the given transactions and EndBlock
// events. Each transaction gets a signature of its own, so two identical
// messages still make two transactions.
func queueInput(t *testing.T, txs []func(*testing.T) (string, node.TxResult), events ...node.Event) transform.Input {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	for i, tx := range txs {
		raw, res := tx(t)
		b, err := base64.StdEncoding.DecodeString(raw)
		require.NoError(t, err)
		var signed sdktx.TxRaw
		require.NoError(t, signed.Unmarshal(b))
		signed.Signatures = [][]byte{{byte(i)}}
		b, err = signed.Marshal()
		require.NoError(t, err)
		in.Block.Txs = append(in.Block.Txs, base64.StdEncoding.EncodeToString(b))
		in.Results.TxsResults = append(in.Results.TxsResults, res)
	}
	in.Results.FinalizeBlockEvents = events
	return in
}

func create(owner, mkt, side, amount, price string) func(*testing.T) (string, node.TxResult) {
	return func(t *testing.T) (string, node.TxResult) { return createTx(t, owner, mkt, side, amount, price) }
}

// attribution returns, per outcome in event order, the index of the
// transaction it is attributed to, or -1.
func attribution(t *testing.T, in transform.Input) (orders, fills []int) {
	t.Helper()
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	index := map[string]int{"": -1}
	for _, tx := range ents.Transactions {
		index[tx.Hash] = tx.TxIndex
	}
	for _, o := range ents.Orders {
		orders = append(orders, index[o.CreatedTxHash])
	}
	for _, f := range ents.OrderFills {
		fills = append(fills, index[f.TakerTxHash])
	}
	return orders, fills
}

func TestQueueOrderAttribution(t *testing.T) {
	t.Run("two identical orders of one owner: in transaction order", func(t *testing.T) {
		orders, _ := attribution(t, queueInput(t,
			[]func(*testing.T) (string, node.TxResult){
				create(trader, market, "sell", "100", "2"), create(trader, market, "sell", "100", "2"),
			},
			saved(msgID(1), market, "sell", "100", "2", trader), saved(msgID(2), market, "sell", "100", "2", trader)))
		assert.Equal(t, []int{0, 1}, orders)
	})

	t.Run("the remainder after the fills, then the next order", func(t *testing.T) {
		orders, fills := attribution(t, queueInput(t,
			[]func(*testing.T) (string, node.TxResult){
				create(trader, market, "buy", "100", "2"), create(trader, market, "buy", "100", "2"),
			},
			executed(msgID(9), market, "sell", "30", "2", trader), saved(msgID(1), market, "buy", "70", "2", trader),
			saved(msgID(2), market, "buy", "100", "2", trader)))
		assert.Equal(t, []int{0, 1}, orders)
		assert.Equal(t, []int{0}, fills)
	})

	t.Run("an outcome of a message queued earlier stays unattributed", func(t *testing.T) {
		orders, fills := attribution(t, queueInput(t,
			[]func(*testing.T) (string, node.TxResult){create(trader, market, "sell", "100", "2")},
			executed(msgID(8), market, "buy", "5", "7", maker1), saved(msgID(1), market, "sell", "100", "2", trader)))
		assert.Equal(t, []int{0}, orders)
		assert.Equal(t, []int{-1}, fills)
	})

	t.Run("a message refunded without an outcome claims nothing", func(t *testing.T) {
		orders, _ := attribution(t, queueInput(t,
			[]func(*testing.T) (string, node.TxResult){
				create(maker1, market, "sell", "1", "2"), create(trader, market, "sell", "100", "3"),
			},
			saved(msgID(1), market, "sell", "100", "3", trader)))
		assert.Equal(t, []int{1}, orders)
	})

	t.Run("each market's queue on its own, whatever order the markets run in (v8.1)", func(t *testing.T) {
		orders, _ := attribution(t, queueInput(t,
			[]func(*testing.T) (string, node.TxResult){
				create(trader, otherMarket, "sell", "100", "2"), create(trader, market, "sell", "100", "2"),
			},
			saved(msgID(2), market, "sell", "100", "2", trader), saved(msgID(1), otherMarket, "sell", "100", "2", trader)))
		assert.Equal(t, []int{1, 0}, orders)
	})

	t.Run("chain v7.0 names no owner: market, side, price and amount", func(t *testing.T) {
		orders, fills := attribution(t, queueInput(t,
			[]func(*testing.T) (string, node.TxResult){create(trader, market, "buy", "100", "2")},
			executed(msgID(9), market, "sell", "40", "2", ""), saved(msgID(1), market, "buy", "60", "2", "")))
		assert.Equal(t, []int{0}, orders)
		assert.Equal(t, []int{0}, fills)
	})

	t.Run("a MsgFillOrders takes its fills, never a saved order", func(t *testing.T) {
		fillOrders := func(t *testing.T) (string, node.TxResult) {
			return encodedTx(t, &tradebintypes.MsgFillOrders{Creator: trader, MarketId: market, OrderType: "sell",
				Orders: []*tradebintypes.FillOrderItem{{Price: "2", Amount: "10"}, {Price: "3", Amount: "20"}}}), node.TxResult{}
		}
		orders, fills := attribution(t, queueInput(t,
			[]func(*testing.T) (string, node.TxResult){fillOrders, create(trader, market, "buy", "50", "3")},
			executed(msgID(7), market, "sell", "10", "2", trader), executed(msgID(8), market, "sell", "20", "3", trader),
			saved(msgID(1), market, "buy", "50", "3", trader)))
		assert.Equal(t, []int{0, 0}, fills)
		assert.Equal(t, []int{1}, orders)
	})

	t.Run("a failed transaction queued nothing", func(t *testing.T) {
		in := queueInput(t,
			[]func(*testing.T) (string, node.TxResult){create(trader, market, "sell", "100", "2")},
			saved(msgID(1), market, "sell", "100", "2", trader))
		in.Results.TxsResults[0].Code = 5
		orders, _ := attribution(t, in)
		assert.Equal(t, []int{-1}, orders)
	})
}
