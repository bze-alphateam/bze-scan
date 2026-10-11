package transform_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// goldenDex is the order book rows of one height as the golden files store
// them.
type goldenDex struct {
	OrderMessages []transform.OrderMessage `json:"order_messages"`
	Orders        []transform.Order        `json:"orders"`
	OrderFills    []transform.OrderFill    `json:"order_fills"`
	OrderCancels  []transform.OrderCancel  `json:"order_cancels"`
}

// The recorded heights predate chain v8.2.0, so their order book rows carry
// no message id and are attributed by queue order. Heights without order
// book rows have no golden file.
func TestDexGolden(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	withRows := 0
	for _, h := range n.FixtureHeights() {
		t.Run(fmt.Sprint(h), func(t *testing.T) {
			ents, err := tr.Transform(fetchInput(t, n, h))
			require.NoError(t, err)
			file := filepath.Join("testdata", fmt.Sprintf("dex_%d.golden.json", h))
			g := goldenDex{ents.OrderMessages, ents.Orders, ents.OrderFills, ents.OrderCancels}
			if len(g.OrderMessages)+len(g.Orders)+len(g.OrderFills)+len(g.OrderCancels) == 0 {
				_, err := os.Stat(file)
				assert.True(t, os.IsNotExist(err), "no rows, no golden file")
				return
			}
			withRows++
			assert.Empty(t, g.OrderMessages, "no message ids before chain v8.2.0")
			for _, o := range g.Orders {
				assert.Empty(t, o.MessageID)
			}
			got, err := json.MarshalIndent(g, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')
			if *update {
				require.NoError(t, os.WriteFile(file, got, 0o644))
			}
			want, err := os.ReadFile(file)
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(got))
		})
	}
	assert.Equal(t, 11, withRows, "the recorded heights with order book events")
}

// Recorded heights cover the shapes: a sell that rests on the book and a
// buy that fills an older order and the new one in the same EndBlock
// (20230045, chain v7 normalised by the archive adapter), cancels
// (24998321) and a fill of a resting order (24999134).
func TestFixtureOrders(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	ents, err := tr.Transform(fetchInput(t, n, 20230045))
	require.NoError(t, err)
	require.Len(t, ents.Orders, 1)
	require.Len(t, ents.OrderFills, 2)
	o := ents.Orders[0]
	assert.Equal(t, int64(20230045), o.Height)
	assert.NotEmpty(t, o.OrderID)
	assert.Contains(t, []string{"buy", "sell"}, o.Side)
	for _, f := range ents.OrderFills {
		assert.NotEmpty(t, f.Maker)
		assert.NotEmpty(t, f.Taker)
		assert.NotEmpty(t, f.Amount)
		assert.Empty(t, f.TakerMessageID)
	}

	ents, err = tr.Transform(fetchInput(t, n, 24998321))
	require.NoError(t, err)
	assert.Len(t, ents.OrderCancels, 11)
	assert.Empty(t, ents.Orders)

	ents, err = tr.Transform(fetchInput(t, n, 24999134))
	require.NoError(t, err)
	require.Len(t, ents.OrderFills, 1)
	f := ents.OrderFills[0]
	assert.Equal(t, "000000000000000006704522", f.OrderID)
	assert.Equal(t, "763662113", f.Amount)
	assert.Equal(t, "0.0002", f.Price)
	assert.Equal(t, "buy", f.Side)
	var seqs []int
	for _, e := range ents.BlockEvents {
		if e.Type == "bze.tradebin.OrderExecutedEvent" {
			seqs = append(seqs, e.Seq)
		}
	}
	assert.Equal(t, []int{f.Seq}, seqs, "a fill's seq is its block_events seq")
}

const (
	trader = "bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk"
	maker1 = "bze15pqjgk4la0mfphwddce00d05n3th3u66n3ptcv"
	market = "factory/x/uhoney/ubze"
)

// msgID is queue message id n, zero-filled to 24 digits.
func msgID(n int) string { return fmt.Sprintf("%024d", n) }

// dexInput is a chain v8.2.0 block: a create that fills two orders and
// rests its remainder, a cancel, a MsgFillOrders filling two orders, and a
// fill of a message queued in an earlier block. No recorded height has
// message ids yet (mainnet runs v8.1.1, the testnet had no order activity
// since its upgrade), so the transactions are encoded with the chain codec
// and given the events the chain emits.
func dexInput(t *testing.T) transform.Input {
	in := baseInput()
	in.Block.Txs = []string{
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "500", Price: "2", MarketId: market}),
		encodedTx(t, &tradebintypes.MsgCancelOrder{Creator: trader, MarketId: market, OrderId: msgID(800), OrderType: "sell"}),
		encodedTx(t, &tradebintypes.MsgFillOrders{Creator: trader, MarketId: market, OrderType: "buy",
			Orders: []*tradebintypes.FillOrderItem{{Price: "3", Amount: "10"}, {Price: "4", Amount: "20"}}}),
	}
	in.Results.TxsResults = []node.TxResult{
		{Events: []node.Event{typed("bze.tradebin.OrderCreateMessageEvent", "0", "creator", trader, "market_id", market,
			"order_type", "buy", "amount", "500", "price", "2", "message_id", msgID(1))}},
		{Events: []node.Event{typed("bze.tradebin.OrderCancelMessageEvent", "0", "creator", trader, "marketId", market,
			"orderId", msgID(800), "order_type", "sell", "message_id", msgID(2))}},
		{Events: []node.Event{
			typed("bze.tradebin.OrderCreateMessageEvent", "0", "creator", trader, "market_id", market,
				"order_type", "buy", "amount", "10", "price", "3", "message_id", msgID(3)),
			typed("bze.tradebin.OrderCreateMessageEvent", "0", "creator", trader, "market_id", market,
				"order_type", "buy", "amount", "20", "price", "4", "message_id", msgID(4)),
		}},
	}
	fill := func(order, amount, price, id string) node.Event {
		return typed("bze.tradebin.OrderExecutedEvent", "", "id", order, "market_id", market, "order_type", "sell",
			"amount", amount, "price", price, "maker", maker1, "taker", trader, "message_id", id)
	}
	in.Results.FinalizeBlockEvents = []node.Event{
		fill(msgID(900), "100", "1.5", msgID(1)),
		fill(msgID(901), "150", "2", msgID(1)),
		typed("bze.tradebin.OrderSavedEvent", "", "id", msgID(950), "market_id", market, "order_type", "buy",
			"amount", "250", "price", "2", "owner", trader, "message_id", msgID(1)),
		typed("bze.tradebin.OrderCanceledEvent", "", "id", msgID(800), "market_id", market, "order_type", "sell",
			"amount", "40", "price", "9", "owner", trader, "message_id", msgID(2)),
		fill(msgID(902), "10", "3", msgID(3)),
		fill(msgID(903), "20", "4", msgID(4)),
		fill(msgID(904), "5", "5", msgID(77)),
	}
	return in
}

func TestOrdersAttributedInTheirBlock(t *testing.T) {
	in := dexInput(t)
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.Transactions, 3)
	create, cancel, fills := ents.Transactions[0].Hash, ents.Transactions[1].Hash, ents.Transactions[2].Hash
	at := time.Unix(100, 0).UTC()

	assert.Equal(t, []transform.OrderMessage{
		{Height: 10, MessageID: msgID(1), TxIndex: 0, MsgIndex: 0, TxHash: create, Kind: "create"},
		{Height: 10, MessageID: msgID(2), TxIndex: 1, MsgIndex: 0, TxHash: cancel, Kind: "cancel"},
		{Height: 10, MessageID: msgID(3), TxIndex: 2, MsgIndex: 0, TxHash: fills, Kind: "fill"},
		{Height: 10, MessageID: msgID(4), TxIndex: 2, MsgIndex: 0, TxHash: fills, Kind: "fill"},
	}, ents.OrderMessages)
	assert.Equal(t, []transform.Order{{
		OrderID: msgID(950), MarketID: market, Owner: trader, Side: "buy", Price: "2", Amount: "250",
		Height: 10, Time: at, MessageID: msgID(1), CreatedTxHash: create,
	}}, ents.Orders, "the remainder rests on the book, placed by the create")
	fill := func(seq int, order, amount, price, id, tx string) transform.OrderFill {
		return transform.OrderFill{Height: 10, Seq: seq, OrderID: order, MarketID: market, Side: "sell", Price: price,
			Amount: amount, Maker: maker1, Taker: trader, TakerMessageID: id, TakerTxHash: tx, Time: at}
	}
	assert.Equal(t, []transform.OrderFill{
		fill(0, msgID(900), "100", "1.5", msgID(1), create),
		fill(1, msgID(901), "150", "2", msgID(1), create),
		fill(4, msgID(902), "10", "3", msgID(3), fills),
		fill(5, msgID(903), "20", "4", msgID(4), fills),
		fill(6, msgID(904), "5", "5", msgID(77), ""),
	}, ents.OrderFills, "a message queued in an earlier block stays unattributed here")
	assert.Equal(t, []transform.OrderCancel{{OrderID: msgID(800), Height: 10, Time: at}}, ents.OrderCancels)
}

// A failed transaction queued nothing: its order events, had it any, are
// no order messages.
func TestFailedTransactionQueuesNoOrderMessage(t *testing.T) {
	in := dexInput(t)
	in.Results.TxsResults[0].Code = 5
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	for _, m := range ents.OrderMessages {
		assert.NotEqual(t, 0, m.TxIndex)
	}
	assert.Empty(t, ents.Orders[0].CreatedTxHash)
}
