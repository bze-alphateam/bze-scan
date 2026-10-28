package transform

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	sdkmath "cosmossdk.io/math"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// A trade is an order book fill or a liquidity-pool swap. Every trade gets a
// trade id that never changes: height × TradeIDsPerBlock + n, n being the
// trade's 1-based ordinal among all the trades of its block, first the swaps
// of the transactions (by tx_index, msg_index, then position among the
// message's swap events), then the block-level trades, fills and the chain's
// own swaps, by block_events seq. Migration 000023's explorer.index_trades
// numbers the stored rows by the same rule.

// TradeIDsPerBlock is the multiplier of the height in a trade id; a block
// holds at most TradeIDsPerBlock − 1 trades.
const TradeIDsPerBlock = 1_000_000

// evSwap is the event of a liquidity-pool swap, emitted by a transaction's
// swap messages and by the chain itself (fee conversions, the burner).
const evSwap = "bze.tradebin.SwapEvent"

// Sides of a swap, from the base's point of view.
const (
	SwapSell = "sell"
	SwapBuy  = "buy"
)

// Swap is one explorer.swaps row: a SwapEvent.
type Swap struct {
	Height  int64     `json:"height"`
	TradeID int64     `json:"trade_id"`
	Time    time.Time `json:"time"`
	PoolID  string    `json:"pool_id"`
	// Base is the byte-wise smaller of the two denoms, Quote the other, as
	// in the pool id.
	Base  string `json:"base"`
	Quote string `json:"quote"`
	// Side is SwapSell when the swap paid in base, SwapBuy otherwise.
	Side        string `json:"side"`
	BaseAmount  string `json:"base_amount"`
	QuoteAmount string `json:"quote_amount"`
	Creator     string `json:"creator"`
	// TxHash, TxIndex and MsgIndex locate a transaction's swap; BlockSeq is
	// the block_events seq of a block-level one.
	TxHash   string `json:"tx_hash,omitempty"`
	TxIndex  *int   `json:"tx_index,omitempty"`
	MsgIndex *int   `json:"msg_index,omitempty"`
	BlockSeq *int   `json:"block_seq,omitempty"`
}

// swapCoin is the in or out attribute of a SwapEvent.
type swapCoin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// newSwap reads a SwapEvent of block b.
func newSwap(b Block, ev node.Event) (Swap, error) {
	attrs := storedEvent(ev).Attrs
	coin := func(key string) (swapCoin, error) {
		raw, err := json.Marshal(attrs[key])
		if err != nil {
			return swapCoin{}, err
		}
		var c swapCoin
		if err := json.Unmarshal(raw, &c); err != nil {
			return swapCoin{}, fmt.Errorf("swap %s: %w", key, err)
		}
		if _, ok := sdkmath.NewIntFromString(c.Amount); !ok || c.Denom == "" {
			return swapCoin{}, fmt.Errorf("swap %s %q%s is not a coin", key, c.Amount, c.Denom)
		}
		return c, nil
	}
	in, err := coin("in")
	if err != nil {
		return Swap{}, err
	}
	out, err := coin("out")
	if err != nil {
		return Swap{}, err
	}
	s := Swap{Height: b.Height, Time: b.Time, PoolID: anyString(attrs["pool_id"]), Creator: anyString(attrs["creator"])}
	if in.Denom < out.Denom {
		s.Base, s.Quote, s.Side, s.BaseAmount, s.QuoteAmount = in.Denom, out.Denom, SwapSell, in.Amount, out.Amount
	} else {
		s.Base, s.Quote, s.Side, s.BaseAmount, s.QuoteAmount = out.Denom, in.Denom, SwapBuy, out.Amount, in.Amount
	}
	return s, nil
}

// blockSwaps returns the swaps of the block's finalize events, with their
// seq.
func blockSwaps(b Block, events []node.Event) ([]Swap, error) {
	var out []Swap
	for i, ev := range events {
		if ev.Type != evSwap {
			continue
		}
		s, err := newSwap(b, ev)
		if err != nil {
			return nil, fmt.Errorf("block event %d: %w", i, err)
		}
		s.BlockSeq = &i
		out = append(out, s)
	}
	return out, nil
}

// txSwaps appends the swaps of a successful transaction, by message and
// emission order.
func txSwaps(ents *Entities, b Block, txIndex int, txHash string, msgCount int, byIndex map[int][]node.Event) error {
	for j := range msgCount {
		for _, ev := range byIndex[j] {
			if ev.Type != evSwap {
				continue
			}
			s, err := newSwap(b, ev)
			if err != nil {
				return fmt.Errorf("msg %d: %w", j, err)
			}
			s.TxHash, s.TxIndex, s.MsgIndex = txHash, &txIndex, &j
			ents.Swaps = append(ents.Swaps, s)
		}
	}
	return nil
}

// checkTradeCount fails a block with TradeIDsPerBlock trades or more, before
// any work on it: its fills, its block-level swaps and the swaps of its
// successful transactions.
func checkTradeCount(r *node.BlockResults) error {
	n := 0
	for _, ev := range r.FinalizeBlockEvents {
		if ev.Type == evSwap || ev.Type == evOrderExecuted {
			n++
		}
	}
	for _, tx := range r.TxsResults {
		if tx.Code != 0 {
			continue
		}
		for _, ev := range tx.Events {
			if ev.Type == evSwap {
				n++
			}
		}
	}
	if n >= TradeIDsPerBlock {
		return fmt.Errorf("%d trades in the block, more than %d", n, TradeIDsPerBlock-1)
	}
	return nil
}

// assignTradeIDs numbers the trades of block height: ents.Swaps holds the
// block's transaction swaps in order, block its block-level swaps; the
// block-level swaps are appended to ents.Swaps.
func assignTradeIDs(ents *Entities, height int64, block []Swap) error {
	n := int64(0)
	next := func() (int64, error) {
		n++
		if n >= TradeIDsPerBlock {
			return 0, fmt.Errorf("more than %d trades in the block", TradeIDsPerBlock-1)
		}
		return height*TradeIDsPerBlock + n, nil
	}
	var err error
	for i := range ents.Swaps {
		if ents.Swaps[i].TradeID, err = next(); err != nil {
			return err
		}
	}
	// The fills and the block-level swaps, merged by seq.
	type at struct {
		seq  int
		fill int // index in ents.OrderFills, −1 for a swap
		swap int
	}
	var level []at
	for i, f := range ents.OrderFills {
		level = append(level, at{seq: f.Seq, fill: i})
	}
	for i, s := range block {
		level = append(level, at{seq: *s.BlockSeq, fill: -1, swap: i})
	}
	slices.SortFunc(level, func(a, b at) int { return a.seq - b.seq })
	for _, t := range level {
		id, err := next()
		if err != nil {
			return err
		}
		if t.fill >= 0 {
			ents.OrderFills[t.fill].TradeID = id
		} else {
			block[t.swap].TradeID = id
		}
	}
	ents.Swaps = append(ents.Swaps, block...)
	return nil
}
