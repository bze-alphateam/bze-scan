package transform_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

const (
	swapper   = "bze1swapper"
	burnerAcc = "bze1burner"
	uatom     = "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2"
	uhoney    = "factory/bze1creator/uhoney"
)

// swapEvent is a SwapEvent as the chain emits it: JSON attribute values;
// msgIndex "" for a block-level one.
func swapEvent(msgIndex, creator, inAmount, inDenom, outAmount, outDenom string) node.Event {
	pool := inDenom + "_" + outDenom
	if outDenom < inDenom {
		pool = outDenom + "_" + inDenom
	}
	ev := node.Event{Type: "bze.tradebin.SwapEvent", Attributes: []node.Attribute{
		{Key: "creator", Value: `"` + creator + `"`},
		{Key: "in", Value: fmt.Sprintf(`{"denom":%q,"amount":%q}`, inDenom, inAmount)},
		{Key: "out", Value: fmt.Sprintf(`{"denom":%q,"amount":%q}`, outDenom, outAmount)},
		{Key: "pool_id", Value: `"` + pool + `"`},
	}}
	if msgIndex != "" {
		ev = withIndex(ev, msgIndex)
	}
	return ev
}

// swapInput is a block with a plain swap (tx 0), a failed swap (tx 1), a
// multi-hop swap of two hops after a send (tx 2, message 1), and, at block
// level, a fill, a burner swap and a second fill.
func swapInput(t *testing.T) transform.Input {
	in := baseInput()
	in.Block.Txs = []string{
		encodedTx(t, &tradebintypes.MsgMultiSwap{Creator: swapper, Routes: []string{uatom + "_ubze"},
			Input: sdk.NewInt64Coin("ubze", 1000)}),
		encodedTx(t, &tradebintypes.MsgMultiSwap{Creator: swapper, Routes: []string{uatom + "_ubze"},
			Input: sdk.NewInt64Coin("ubze", 7)}),
		encodedTx(t,
			&tradebintypes.MsgCancelOrder{Creator: swapper, MarketId: market, OrderId: msgID(1), OrderType: "buy"},
			&tradebintypes.MsgMultiSwap{Creator: swapper, Routes: []string{uatom + "_ubze", uhoney + "_ubze"},
				Input: sdk.NewInt64Coin(uatom, 50)}),
	}
	in.Results.TxsResults = []node.TxResult{
		{Events: []node.Event{swapEvent("0", swapper, "1000", "ubze", "40", uatom)}},
		{Code: 5, Events: []node.Event{swapEvent("0", swapper, "7", "ubze", "1", uatom)}},
		{Events: []node.Event{
			swapEvent("1", swapper, "50", uatom, "1200", "ubze"),
			swapEvent("1", swapper, "1200", "ubze", "9", uhoney),
		}},
	}
	fill := func(order string) node.Event {
		return typed("bze.tradebin.OrderExecutedEvent", "", "id", order, "market_id", market, "order_type", "sell",
			"amount", "10", "price", "2", "maker", maker1, "taker", trader)
	}
	in.Results.FinalizeBlockEvents = []node.Event{
		fill(msgID(900)),
		swapEvent("", burnerAcc, "300", uhoney, "5", "ubze"),
		fill(msgID(901)),
	}
	return in
}

func TestSwapsAndTradeIDs(t *testing.T) {
	in := swapInput(t)
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.Transactions, 3)
	at := time.Unix(100, 0).UTC()
	id := func(n int64) int64 { return 10*transform.TradeIDsPerBlock + n }
	plain, multi := ents.Transactions[0].Hash, ents.Transactions[2].Hash

	assert.Equal(t, []transform.Swap{
		{Height: 10, TradeID: id(1), Time: at, PoolID: uatom + "_ubze", Base: uatom, Quote: "ubze", Side: "buy",
			BaseAmount: "40", QuoteAmount: "1000", Creator: swapper, TxHash: plain, TxIndex: intp(0), MsgIndex: intp(0)},
		// The failed transaction swapped nothing; the multi-hop swap is two
		// trades, in hop order.
		{Height: 10, TradeID: id(2), Time: at, PoolID: uatom + "_ubze", Base: uatom, Quote: "ubze", Side: "sell",
			BaseAmount: "50", QuoteAmount: "1200", Creator: swapper, TxHash: multi, TxIndex: intp(2), MsgIndex: intp(1)},
		{Height: 10, TradeID: id(3), Time: at, PoolID: uhoney + "_ubze", Base: uhoney, Quote: "ubze", Side: "buy",
			BaseAmount: "9", QuoteAmount: "1200", Creator: swapper, TxHash: multi, TxIndex: intp(2), MsgIndex: intp(1)},
		// The chain's own swap comes after every transaction's, between the
		// fills by block event order.
		{Height: 10, TradeID: id(5), Time: at, PoolID: uhoney + "_ubze", Base: uhoney, Quote: "ubze", Side: "sell",
			BaseAmount: "300", QuoteAmount: "5", Creator: burnerAcc, BlockSeq: intp(1)},
	}, ents.Swaps)
	require.Len(t, ents.OrderFills, 2)
	assert.Equal(t, id(4), ents.OrderFills[0].TradeID)
	assert.Equal(t, id(6), ents.OrderFills[1].TradeID)
}

// The same block transformed twice numbers its trades the same.
func TestTradeIDsAreStable(t *testing.T) {
	first, err := realTransformer(t).Transform(swapInput(t))
	require.NoError(t, err)
	second, err := realTransformer(t).Transform(swapInput(t))
	require.NoError(t, err)
	assert.Equal(t, first.Swaps, second.Swaps)
	assert.Equal(t, first.OrderFills, second.OrderFills)
}

// A block without trades has no swaps.
func TestNoTradesNoSwaps(t *testing.T) {
	ents, err := realTransformer(t).Transform(baseInput())
	require.NoError(t, err)
	assert.Empty(t, ents.Swaps)
}

// Trade ids hold 999,999 trades per block: a block with a million fails
// before any work on it, the fills and the swaps of its successful
// transactions counted.
func TestTradeIDGuard(t *testing.T) {
	build := func(fills, swaps int, txCode uint32) transform.Input {
		in := baseInput()
		in.Block.Txs = []string{"YQ=="}
		in.Results.TxsResults = []node.TxResult{{Code: txCode,
			Events: slices.Repeat([]node.Event{swapEvent("0", swapper, "3", uhoney, "1", "ubze")}, swaps)}}
		in.Results.FinalizeBlockEvents = slices.Repeat([]node.Event{{Type: "bze.tradebin.OrderExecutedEvent"}}, fills)
		return in
	}
	tr, _ := mockTransformer(mockDecoder{})
	_, err := tr.Transform(build(transform.TradeIDsPerBlock-1, 1, 0))
	require.ErrorContains(t, err, "1000000 trades in the block, more than 999999")
	_, err = tr.Transform(build(0, transform.TradeIDsPerBlock, 0))
	require.ErrorContains(t, err, "1000000 trades in the block")

	// A swap event of no message is never numbered, so it does not count.
	in := build(transform.TradeIDsPerBlock-1, 0, 0)
	in.Results.TxsResults[0].Events = []node.Event{swapEvent("", swapper, "3", uhoney, "1", "ubze")}
	_, err = tr.Transform(in)
	require.NoError(t, err)

	// A failed transaction swapped nothing.
	ents, err := tr.Transform(build(2, 3, 5))
	require.NoError(t, err)
	assert.Equal(t, int64(10*transform.TradeIDsPerBlock+2), ents.OrderFills[1].TradeID)
}

// A swap event that names no coin fails the block rather than shifting the
// ids of the trades after it.
func TestMalformedSwapFailsTheBlock(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = []node.Event{{Type: "bze.tradebin.SwapEvent", Attributes: []node.Attribute{
		{Key: "in", Value: `{"denom":"ubze","amount":"x"}`}}}}
	tr, _ := mockTransformer(mockDecoder{})
	_, err := tr.Transform(in)
	assert.ErrorContains(t, err, "swap in")
}
