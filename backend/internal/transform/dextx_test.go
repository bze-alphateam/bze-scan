package transform_test

import (
	"slices"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// Chain v8.3.0 runs the order book inside the message: the outcomes are the
// transaction's own events and nothing is queued.

// txExecuted is an OrderExecutedEvent of a message: the maker order, the
// taker's own order id (order_id).
func txExecuted(msgIndex, makerOrder, takerOrder, amount, price string) node.Event {
	return typed("bze.tradebin.OrderExecutedEvent", msgIndex, "id", makerOrder, "market_id", market, "order_type", "sell",
		"amount", amount, "price", price, "maker", maker1, "taker", trader, "order_id", takerOrder)
}

func txSaved(msgIndex, id, amount string) node.Event {
	return typed("bze.tradebin.OrderSavedEvent", msgIndex, "id", id, "market_id", market, "order_type", "buy",
		"amount", amount, "price", "2", "owner", trader, "order_id", id)
}

func txCanceled(msgIndex, id string) node.Event {
	return typed("bze.tradebin.OrderCanceledEvent", msgIndex, "id", id, "market_id", market, "order_type", "sell",
		"amount", "40", "price", "9", "owner", trader, "order_id", id)
}

// noise are the events every message emits besides its typed ones: they are
// not stored with the message and take no position.
func noise(msgIndex string) []node.Event {
	return []node.Event{
		withIndex(transfer(trader, maker1, "5ubze"), msgIndex),
		withIndex(node.Event{Type: "message", Attributes: attrs("action", "x")}, msgIndex),
	}
}

func orderEvents(parts ...[]node.Event) []node.Event {
	var out []node.Event
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func one(ev node.Event) []node.Event { return []node.Event{ev} }

// v830Input is a block of chain v8.3.0 order transactions: a create filling
// two makers and resting the rest (tx 0), a create filled in full (tx 1), a
// MsgFillOrders of two entries (tx 2), a cancel (tx 3) and a failed create
// (tx 4).
func v830Input(t *testing.T) transform.Input {
	in := baseInput()
	in.Block.Txs = []string{
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "500", Price: "2", MarketId: market}),
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "50", Price: "2", MarketId: market}),
		encodedTx(t, &tradebintypes.MsgFillOrders{Creator: trader, MarketId: market, OrderType: "buy",
			Orders: []*tradebintypes.FillOrderItem{{Price: "3", Amount: "10"}, {Price: "4", Amount: "20"}}}),
		encodedTx(t, &tradebintypes.MsgCancelOrder{Creator: trader, MarketId: market, OrderId: msgID(800), OrderType: "sell"}),
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "5", Price: "2", MarketId: market}),
	}
	in.Results.TxsResults = []node.TxResult{
		{Events: orderEvents(noise("0"), one(txExecuted("0", msgID(900), msgID(1000), "100", "1.5")),
			one(txExecuted("0", msgID(901), msgID(1000), "150", "2")), one(txSaved("0", msgID(1000), "250")))},
		{Events: orderEvents(one(txExecuted("0", msgID(902), msgID(1001), "50", "2")))},
		{Events: orderEvents(one(txExecuted("0", msgID(903), msgID(1002), "10", "3")),
			one(txExecuted("0", msgID(904), msgID(1003), "20", "4")))},
		{Events: orderEvents(one(txCanceled("0", msgID(800))))},
		{Code: 11, Events: orderEvents(one(txExecuted("0", msgID(905), msgID(1004), "5", "2")),
			one(txSaved("0", msgID(1004), "5")), one(txCanceled("0", msgID(801))))},
	}
	return in
}

func TestTxLevelOrderOutcomes(t *testing.T) {
	ents, err := realTransformer(t).Transform(v830Input(t))
	require.NoError(t, err)
	require.Len(t, ents.Transactions, 5)
	hash := func(i int) string { return ents.Transactions[i].Hash }

	assert.Empty(t, ents.OrderMessages, "nothing is queued: no message ids")
	require.Len(t, ents.OrderFills, 5, "the failed transaction's events are not read")
	type fill struct {
		seq             int
		order, takerOrd string
		tx              string
		txIdx, msgIdx   int
	}
	var got []fill
	for _, f := range ents.OrderFills {
		require.NotNil(t, f.TxIndex)
		require.NotNil(t, f.MsgIndex)
		require.NotNil(t, f.MsgEventIndex)
		assert.Empty(t, f.TakerMessageID)
		assert.Equal(t, maker1, f.Maker)
		assert.Equal(t, trader, f.Taker)
		got = append(got, fill{f.Seq, f.OrderID, f.TakerOrderID, f.TakerTxHash, *f.TxIndex, *f.MsgIndex})
	}
	assert.Equal(t, []fill{
		{1_000_000, msgID(900), msgID(1000), hash(0), 0, 0},
		{1_000_001, msgID(901), msgID(1000), hash(0), 0, 0},
		{1_000_002, msgID(902), msgID(1001), hash(1), 1, 0},
		{1_000_003, msgID(903), msgID(1002), hash(2), 2, 0},
		{1_000_004, msgID(904), msgID(1003), hash(2), 2, 0},
	}, got, "a full-fill taker has fills only; a MsgFillOrders entry has its own taker order id")
	// The transfer and the message event of tx 0 are not stored events: the
	// fills sit at positions 0 and 1 of the stored ones.
	assert.Equal(t, 0, *ents.OrderFills[0].MsgEventIndex)
	assert.Equal(t, 1, *ents.OrderFills[1].MsgEventIndex)

	require.Len(t, ents.Orders, 1)
	o := ents.Orders[0]
	assert.Equal(t, msgID(1000), o.OrderID)
	assert.Equal(t, hash(0), o.CreatedTxHash)
	assert.Empty(t, o.MessageID)
	assert.Equal(t, trader, o.Owner)

	require.Len(t, ents.OrderClosures, 1)
	c := ents.OrderClosures[0]
	assert.Equal(t, transform.OrderClosure{OrderID: msgID(800), Height: 10, Time: c.Time, TxHash: hash(3),
		Reason: transform.OrderClosureCancelled}, c)
	assert.Equal(t, []transform.OrderCancel{{OrderID: msgID(800), Height: 10, Time: c.Time}}, ents.OrderCancels)
}

// The same block transformed twice yields the same rows: a reindex of a
// v8.3.0 height writes what live indexing wrote.
func TestTxLevelOrdersAreStable(t *testing.T) {
	first, err := realTransformer(t).Transform(v830Input(t))
	require.NoError(t, err)
	second, err := realTransformer(t).Transform(v830Input(t))
	require.NoError(t, err)
	assert.Equal(t, first.OrderFills, second.OrderFills)
	assert.Equal(t, first.Orders, second.Orders)
	assert.Equal(t, first.OrderClosures, second.OrderClosures)
	assert.Equal(t, first.Swaps, second.Swaps)
}

// A block with both kinds of outcome, as the v8.3.0 upgrade block can look: a
// queued message of tx 0 with its block-level fill, and tx 1's own fee swap and
// fill. The queue attribution keeps to the block-level rows, the tx rows keep
// their hashes, and the trade ids number the transaction's trades by position,
// then the block-level ones.
func TestQueueHeuristicLeavesTxLevelOutcomesAlone(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "10", Price: "2", MarketId: market}),
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "20", Price: "2", MarketId: market}),
	}
	in.Results.TxsResults = []node.TxResult{
		{Events: one(typed("bze.tradebin.OrderCreateMessageEvent", "0", "creator", trader, "market_id", market,
			"order_type", "buy", "amount", "10", "price", "2", "message_id", msgID(1)))},
		{Events: orderEvents(one(swapEvent("0", trader, "3", uhoney, "1", "ubze")),
			one(txExecuted("0", msgID(902), msgID(1001), "20", "2")))},
	}
	in.Results.FinalizeBlockEvents = []node.Event{
		typed("bze.tradebin.OrderExecutedEvent", "", "id", msgID(905), "market_id", market, "order_type", "sell",
			"amount", "10", "price", "2", "maker", maker1, "taker", trader, "message_id", msgID(1)),
		swapEvent("", burnerAcc, "300", uhoney, "5", "ubze"),
	}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.OrderFills, 2)
	byOrder := map[string]transform.OrderFill{}
	for _, f := range ents.OrderFills {
		byOrder[f.OrderID] = f
	}
	queued, own := byOrder[msgID(905)], byOrder[msgID(902)]
	assert.Nil(t, queued.TxIndex, "a block-level fill has no position")
	assert.Nil(t, queued.MsgEventIndex)
	assert.Equal(t, 0, queued.Seq)
	assert.Equal(t, msgID(1), queued.TakerMessageID)
	assert.Equal(t, ents.Transactions[0].Hash, queued.TakerTxHash, "attributed by its message id")
	assert.Equal(t, transform.TxFillSeqBase, own.Seq)
	assert.Empty(t, own.TakerMessageID)
	assert.Equal(t, ents.Transactions[1].Hash, own.TakerTxHash)
	require.Len(t, ents.OrderMessages, 1)

	id := func(n int64) int64 { return 10*transform.TradeIDsPerBlock + n }
	assert.Equal(t, id(1), ents.Swaps[0].TradeID, "tx 1's swap sits before its fill")
	assert.Equal(t, id(2), own.TradeID)
	assert.Equal(t, id(3), queued.TradeID, "then the block-level trades, by seq")
	assert.Equal(t, id(4), ents.Swaps[1].TradeID)
}

// The seqs of the fills of a block's transactions run on across them.
func TestTxLevelFillSeqsRunAcrossTransactions(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "10", Price: "2", MarketId: market}),
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "20", Price: "2", MarketId: market}),
	}
	in.Results.TxsResults = []node.TxResult{
		{Events: orderEvents(one(txExecuted("0", msgID(900), msgID(1000), "5", "2")), one(txExecuted("0", msgID(901), msgID(1000), "5", "2")))},
		{Events: orderEvents(one(txExecuted("0", msgID(902), msgID(1001), "10", "2")), one(txExecuted("0", msgID(903), msgID(1001), "10", "2")))},
	}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	var seqs []int
	for _, f := range ents.OrderFills {
		seqs = append(seqs, f.Seq)
	}
	assert.Equal(t, []int{1_000_000, 1_000_001, 1_000_002, 1_000_003}, seqs)
}

// NumberTrades needs the positions of the transaction's trades.
func TestNumberTradesNeedsPositions(t *testing.T) {
	_, err := transform.NumberTrades(10, []transform.Swap{{TxIndex: intp(0), MsgIndex: intp(0)}}, nil, nil)
	require.ErrorContains(t, err, "transaction swap 0 has no position")
	_, err = transform.NumberTrades(10, nil, nil, []transform.OrderFill{{TxIndex: intp(0)}})
	require.ErrorContains(t, err, "transaction fill 0 has no position")
	_, err = transform.NumberTrades(10, nil, []transform.Swap{{}}, nil)
	require.ErrorContains(t, err, "block-level swap 0 has no seq")
}

// The trade count of a block counts the fills of its transactions' events.
func TestTradeCountCountsTxLevelFills(t *testing.T) {
	build := func(n int, code uint32) transform.Input {
		in := baseInput()
		in.Block.Txs = []string{"YQ=="}
		in.Results.TxsResults = []node.TxResult{{Code: code,
			Events: slices.Repeat([]node.Event{txExecuted("0", msgID(900), msgID(1000), "1", "2")}, n)}}
		return in
	}
	tr, _ := mockTransformer(mockDecoder{})
	_, err := tr.Transform(build(transform.TradeIDsPerBlock, 0))
	require.ErrorContains(t, err, "1000000 trades in the block, more than 999999")
	_, err = tr.Transform(build(transform.TradeIDsPerBlock, 5))
	require.NoError(t, err, "a failed transaction's events count for nothing")
}

// Trade ids follow the transaction, then the message, then the event's
// position among the message's stored events: two order transactions with a
// swap between them, and a fee swap between two fills of one message.
func TestTxLevelTradeIDs(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "500", Price: "2", MarketId: market}),
		encodedTx(t, &tradebintypes.MsgMultiSwap{Creator: swapper, Routes: []string{uatom + "_ubze"},
			Input: sdk.NewInt64Coin("ubze", 1000)}),
		encodedTx(t, &tradebintypes.MsgFillOrders{Creator: trader, MarketId: market, OrderType: "buy",
			Orders: []*tradebintypes.FillOrderItem{{Price: "3", Amount: "10"}, {Price: "4", Amount: "20"}}}),
	}
	in.Results.TxsResults = []node.TxResult{
		{Events: orderEvents(one(txExecuted("0", msgID(900), msgID(1000), "100", "1.5")),
			one(txExecuted("0", msgID(901), msgID(1000), "150", "2")))},
		{Events: []node.Event{swapEvent("0", swapper, "1000", "ubze", "40", uatom)}},
		// The fee conversion of the maker fee comes between two fills.
		{Events: orderEvents(one(txExecuted("0", msgID(902), msgID(1001), "10", "3")), noise("0"),
			one(swapEvent("0", trader, "3", uhoney, "1", "ubze")), one(txExecuted("0", msgID(903), msgID(1002), "20", "4")))},
	}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	id := func(n int64) int64 { return 10*transform.TradeIDsPerBlock + n }
	var fills []int64
	for _, f := range ents.OrderFills {
		fills = append(fills, f.TradeID)
	}
	assert.Equal(t, []int64{id(1), id(2), id(3 + 1), id(3 + 3)}, fills, "tx 0 fills, tx 1 swap, tx 2: fill, swap, fill")
	require.Len(t, ents.Swaps, 2)
	assert.Equal(t, id(3), ents.Swaps[0].TradeID, "the swap of tx 1")
	assert.Equal(t, id(5), ents.Swaps[1].TradeID, "the fee swap between the fills of tx 2")
	// Stored positions: the fee swap sits between the two fills, the noise
	// events take none.
	assert.Equal(t, []int{0, 1, 2}, []int{*ents.OrderFills[2].MsgEventIndex, *ents.Swaps[1].MsgEventIndex,
		*ents.OrderFills[3].MsgEventIndex})
}

// A block-level swap still numbers after the transaction's trades.
func TestTxLevelTradesComeBeforeBlockLevelOnes(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{
		encodedTx(t, &tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "500", Price: "2", MarketId: market}),
	}
	in.Results.TxsResults = []node.TxResult{{Events: one(txExecuted("0", msgID(900), msgID(1000), "100", "1.5"))}}
	in.Results.FinalizeBlockEvents = []node.Event{swapEvent("", burnerAcc, "300", uhoney, "5", "ubze")}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	id := func(n int64) int64 { return 10*transform.TradeIDsPerBlock + n }
	assert.Equal(t, id(1), ents.OrderFills[0].TradeID)
	assert.Equal(t, id(2), ents.Swaps[0].TradeID)
}

// The maker is named by the taker's transaction: it gets a participant row of
// its own kind with the fills as details; the taker keeps the kind of the
// message it signed.
func TestMakerActivityOfTxLevelFills(t *testing.T) {
	ents, err := realTransformer(t).Transform(v830Input(t))
	require.NoError(t, err)
	var maker, signer *transform.Activity
	for i, a := range ents.Activity {
		if a.TxIndex != 0 {
			continue
		}
		switch a.Address {
		case maker1:
			maker = &ents.Activity[i]
		case trader:
			signer = &ents.Activity[i]
		}
	}
	require.NotNil(t, maker)
	require.NotNil(t, signer)
	assert.Equal(t, "dex_maker_fill", maker.Kind)
	assert.Equal(t, "dex", maker.Category)
	assert.False(t, maker.IsSigner)
	assert.Equal(t, trader, maker.Counterparty)
	require.Len(t, maker.Details, 2, "both fills of the transaction name the maker")
	assert.Equal(t, "bze.tradebin.OrderExecutedEvent", maker.Details[0].Type)
	assert.Equal(t, msgID(900), maker.Details[0].Attrs["id"])
	assert.Equal(t, "dex_order", signer.Kind)
	assert.Empty(t, signer.Details)
}

// A maker that another message of the transaction names too keeps the kind
// that message gives it: only a maker with no other role is a dex_maker_fill.
func TestMakerWithAnotherRoleKeepsItsKind(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{encodedTx(t,
		&tradebintypes.MsgCreateOrder{Creator: trader, OrderType: "buy", Amount: "100", Price: "2", MarketId: market},
		&banktypes.MsgSend{FromAddress: trader, ToAddress: maker1, Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 5))},
	)}
	in.Results.TxsResults = []node.TxResult{{Events: one(txExecuted("0", msgID(900), msgID(1000), "100", "2"))}}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	for _, a := range ents.Activity {
		if a.Address == maker1 {
			assert.Equal(t, "dex_order", a.Kind, "the first message that names it classifies")
			assert.Empty(t, a.Details)
			return
		}
	}
	t.Fatal("no row for the maker")
}
