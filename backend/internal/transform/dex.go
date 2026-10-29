package transform

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	sdkmath "cosmossdk.io/math"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// The tradebin order book works in two steps: a transaction only enqueues a
// message (and emits OrderCreateMessageEvent or OrderCancelMessageEvent), the
// engine processes the queue at EndBlock, normally of the same block, and
// emits the outcome as block-level events. Since chain release v8.2.0 every
// one of these events carries the queue message id, which links a
// transaction to its outcome. Before that release no event carries one,
// and the outcomes are matched to the transactions heuristically (see
// attributeByQueue).

// Typed events of the order book.
const (
	evOrderCreateMessage = "bze.tradebin.OrderCreateMessageEvent"
	evOrderCancelMessage = "bze.tradebin.OrderCancelMessageEvent"
	evOrderSaved         = "bze.tradebin.OrderSavedEvent"
	evOrderExecuted      = "bze.tradebin.OrderExecutedEvent"
	evOrderCanceled      = "bze.tradebin.OrderCanceledEvent"
)

// Type URL of the message that fills resting orders.
const msgFillOrders = "/bze.tradebin.MsgFillOrders"

// Kinds of the order_messages rows.
const (
	OrderMessageCreate = "create"
	OrderMessageCancel = "cancel"
	OrderMessageFill   = "fill"
)

// Statuses of an orders row.
const (
	OrderOpen      = "open"
	OrderFilled    = "filled"
	OrderCancelled = "cancelled"
)

// OrderMessage is one explorer.order_messages row: a transaction-time order
// event carrying a queue message id (chain v8.2.0 and later).
type OrderMessage struct {
	Height int64 `json:"height"`
	// MessageID is the queue message id, 24 zero-filled digits, unique
	// within a block.
	MessageID string `json:"message_id"`
	TxIndex   int    `json:"tx_index"`
	// MsgIndex is the message's index in the transaction; a MsgFillOrders
	// has one row per queued fill under one index.
	MsgIndex int    `json:"msg_index"`
	TxHash   string `json:"tx_hash"`
	// Kind is OrderMessageCreate, OrderMessageCancel or OrderMessageFill.
	Kind string `json:"kind"`
}

// Order is the explorer.orders row of an OrderSavedEvent: an order that
// rested on the book. The writer derives filled_amount and the status.
type Order struct {
	OrderID  string `json:"order_id"`
	MarketID string `json:"market_id"`
	Owner    string `json:"owner"`
	// Side is buy or sell.
	Side  string `json:"side"`
	Price string `json:"price"`
	// Amount is the base-denom amount that rested on the book.
	Amount string    `json:"amount"`
	Height int64     `json:"height"`
	Time   time.Time `json:"time"`
	// MessageID is the queue message id that placed the order; empty before
	// chain v8.2.0.
	MessageID string `json:"message_id"`
	// CreatedTxHash is the transaction that placed the order when it is in
	// the same block; empty otherwise.
	CreatedTxHash string `json:"created_tx_hash"`
	// OwnerFromMessage is true when the event named no owner (chain v7.0
	// and v7.1) and Owner is the creator of the message that placed it: the
	// order's fills named no maker either.
	OwnerFromMessage bool `json:"-"`
}

// OrderFill is one explorer.order_fills row: an OrderExecutedEvent, a queue
// message filling (part of) a resting order.
type OrderFill struct {
	Height int64 `json:"height"`
	// Seq is the event's position in the block's finalize events, the
	// block_events seq.
	Seq      int    `json:"seq"`
	OrderID  string `json:"order_id"`
	MarketID string `json:"market_id"`
	// Side is the side of the resting order.
	Side   string `json:"side"`
	Price  string `json:"price"`
	Amount string `json:"amount"`
	Maker  string `json:"maker"`
	Taker  string `json:"taker"`
	// TakerMessageID is the taker's queue message id; empty before chain
	// v8.2.0.
	TakerMessageID string `json:"taker_message_id"`
	// TakerTxHash is the taker's transaction when it is in the same block;
	// empty otherwise.
	TakerTxHash string    `json:"taker_tx_hash"`
	Time        time.Time `json:"time"`
	// TradeID is the fill's trade id (see TradeIDsPerBlock).
	TradeID int64 `json:"trade_id"`
}

// OrderCancel is an OrderCanceledEvent: the order is closed as cancelled at
// Height.
type OrderCancel struct {
	OrderID string    `json:"order_id"`
	Height  int64     `json:"height"`
	Time    time.Time `json:"time"`
}

// outcome is one order book outcome of a block, in event order: an index
// into Entities.OrderFills or Entities.Orders.
type outcome struct {
	fill bool
	i    int
}

// blockOrders appends the orders, fills and cancels of the block's finalize
// events and returns the fills and saved orders in event order.
func blockOrders(ents *Entities, b Block, events []node.Event) []outcome {
	var seq []outcome
	for i, ev := range events {
		switch ev.Type {
		case evOrderSaved, evOrderExecuted, evOrderCanceled:
		default:
			continue
		}
		attrs := storedEvent(ev).Attrs
		get := func(key string) string { return anyString(attrs[key]) }
		switch ev.Type {
		case evOrderSaved:
			seq = append(seq, outcome{i: len(ents.Orders)})
			ents.Orders = append(ents.Orders, Order{
				OrderID: get("id"), MarketID: get("market_id"), Owner: get("owner"), Side: get("order_type"),
				Price: get("price"), Amount: get("amount"), Height: b.Height, Time: b.Time,
				MessageID: get("message_id"),
			})
		case evOrderExecuted:
			seq = append(seq, outcome{fill: true, i: len(ents.OrderFills)})
			ents.OrderFills = append(ents.OrderFills, OrderFill{
				Height: b.Height, Seq: i, OrderID: get("id"), MarketID: get("market_id"), Side: get("order_type"),
				Price: get("price"), Amount: get("amount"), Maker: get("maker"), Taker: get("taker"),
				TakerMessageID: get("message_id"), Time: b.Time,
			})
		case evOrderCanceled:
			ents.OrderCancels = append(ents.OrderCancels, OrderCancel{OrderID: get("id"), Height: b.Height, Time: b.Time})
		}
	}
	return seq
}

// txOrderMessages appends the order_messages rows of a successful
// transaction: one per order message event carrying a message id. The kind
// comes from the event and, for a create event, from the message's type URL
// (a MsgFillOrders emits one create event per queued fill).
func txOrderMessages(ents *Entities, b Block, txIndex int, txHash string, msgs []chain.Msg,
	byIndex map[int][]node.Event) {
	for j := range msgs {
		for _, ev := range byIndex[j] {
			var kind string
			switch ev.Type {
			case evOrderCreateMessage:
				kind = OrderMessageCreate
				if msgs[j].TypeURL == msgFillOrders {
					kind = OrderMessageFill
				}
			case evOrderCancelMessage:
				kind = OrderMessageCancel
			default:
				continue
			}
			id := attrString(ev, "message_id")
			if id == "" {
				continue
			}
			ents.OrderMessages = append(ents.OrderMessages, OrderMessage{
				Height: b.Height, MessageID: id, TxIndex: txIndex, MsgIndex: j, TxHash: txHash, Kind: kind,
			})
		}
	}
}

// attributeOrders links the block's outcomes to the transactions of the same
// block by message id: the order's placing transaction and the fill's taker
// transaction. Outcomes of messages from earlier blocks stay unattributed.
func attributeOrders(ents *Entities) {
	if len(ents.OrderMessages) == 0 {
		return
	}
	txOf := make(map[string]string, len(ents.OrderMessages))
	for _, m := range ents.OrderMessages {
		if m.Kind != OrderMessageCancel {
			txOf[m.MessageID] = m.TxHash
		}
	}
	for i := range ents.Orders {
		if o := &ents.Orders[i]; o.MessageID != "" {
			o.CreatedTxHash = txOf[o.MessageID]
		}
	}
	for i := range ents.OrderFills {
		if f := &ents.OrderFills[i]; f.TakerMessageID != "" {
			f.TakerTxHash = txOf[f.TakerMessageID]
		}
	}
}

// queued is an order message a transaction enqueued without a message id
// (before chain v8.2.0): a create, or one order of a MsgFillOrders. Side is
// the side the message takes, so the orders it fills rest on the other
// side.
type queued struct {
	owner, market, side, price string
	amount                     sdkmath.Int
	create                     bool
	txHash                     string
}

// fillOrdersBody is the part of a MsgFillOrders body the heuristic reads.
type fillOrdersBody struct {
	Creator   string `json:"creator"`
	MarketID  string `json:"market_id"`
	OrderType string `json:"order_type"`
	Orders    []struct {
		Price  string `json:"price"`
		Amount string `json:"amount"`
	} `json:"orders"`
}

// txQueued appends the messages a successful transaction enqueued without a
// message id, in the order the chain enqueued them: the
// OrderCreateMessageEvent of each create, and the orders of each
// MsgFillOrders body (which emitted no event before chain v8.2.0).
func txQueued(queue []queued, txHash string, msgs []chain.Msg, byIndex map[int][]node.Event) []queued {
	for j, m := range msgs {
		if m.TypeURL == msgFillOrders {
			if hasMessageID(byIndex[j]) || m.Body == nil {
				continue
			}
			var body fillOrdersBody
			if err := json.Unmarshal(m.Body, &body); err != nil {
				continue
			}
			for _, o := range body.Orders {
				amount, ok := sdkmath.NewIntFromString(o.Amount)
				if !ok {
					continue
				}
				queue = append(queue, queued{owner: body.Creator, market: body.MarketID, side: otherSide(body.OrderType),
					price: o.Price, amount: amount, txHash: txHash})
			}
			continue
		}
		for _, ev := range byIndex[j] {
			if ev.Type != evOrderCreateMessage || attrString(ev, "message_id") != "" {
				continue
			}
			amount, ok := sdkmath.NewIntFromString(attrString(ev, "amount"))
			if !ok {
				continue
			}
			queue = append(queue, queued{owner: attrString(ev, "creator"), market: attrString(ev, "market_id"),
				side: attrString(ev, "order_type"), price: attrString(ev, "price"), amount: amount, create: true,
				txHash: txHash})
		}
	}
	return queue
}

func hasMessageID(events []node.Event) bool {
	for _, ev := range events {
		if ev.Type == evOrderCreateMessage && attrString(ev, "message_id") != "" {
			return true
		}
	}
	return false
}

// samePrice reports whether the engine put prices a and b at one price
// level. Since chain v8.1.0 a level is the price as a LegacyDec; before, it
// was the price parsed as a float64 and printed with ten decimals, so a
// message at 209999.999999999979 took the orders resting at 210000. The
// heuristic runs only on blocks before v8.2.0, so it accepts either.
func samePrice(a, b string) bool {
	if a == b {
		return true
	}
	if x, err := sdkmath.LegacyNewDecFromStr(a); err == nil {
		if y, err := sdkmath.LegacyNewDecFromStr(b); err == nil && x.Equal(y) {
			return true
		}
	}
	x, errA := strconv.ParseFloat(a, 64)
	y, errB := strconv.ParseFloat(b, 64)
	return errA == nil && errB == nil && fmt.Sprintf("%024.10f", x) == fmt.Sprintf("%024.10f", y)
}

func otherSide(side string) string {
	if side == "buy" {
		return "sell"
	}
	return "buy"
}

// attributeByQueue links the block's outcomes that carry no message id
// (before chain v8.2.0) to the transactions of the same block, by queue
// order. The engine processes each market's messages in the order they were
// enqueued, which is the transactions' order, and a message's outcome is a
// run of fills of orders resting on the other side at the message's price,
// taken by its owner, then, for a create, the remainder saved as an order:
// the matching walks each market's messages and outcomes together, a
// message claiming the first run at or after the previous match that fits
// it. It is approximate: an outcome of a message queued in an earlier block
// that looks the same can be claimed, and a message whose outcome was only a
// refund claims nothing. Outcomes without a match stay unattributed; chain
// v7.0 and v7.1 events name no owner or taker, so their outcomes match on
// market, side, price and amount alone and take the matched message's
// creator as their owner or taker.
func attributeByQueue(ents *Entities, queue []queued, seq []outcome) {
	if len(queue) == 0 || len(seq) == 0 {
		return
	}
	byMarket := map[string][]outcome{}
	for _, o := range seq {
		if o.fill {
			if f := ents.OrderFills[o.i]; f.TakerMessageID == "" && f.TakerTxHash == "" {
				byMarket[f.MarketID] = append(byMarket[f.MarketID], o)
			}
		} else if ord := ents.Orders[o.i]; ord.MessageID == "" && ord.CreatedTxHash == "" {
			byMarket[ord.MarketID] = append(byMarket[ord.MarketID], o)
		}
	}
	next := map[string]int{}
	for _, q := range queue {
		outs := byMarket[q.market]
		for i := next[q.market]; i < len(outs); i++ {
			if end, ok := claim(ents, q, outs[i:]); ok {
				next[q.market] = i + end
				break
			}
		}
	}
}

// claim attributes to q the run at the start of outs when it fits q: fills
// of the other side at q's price taken by q's owner, adding up to at most
// q's amount, then for a create the order saved with the rest. It returns
// how many outcomes the run holds.
func claim(ents *Entities, q queued, outs []outcome) (int, bool) {
	same := func(owner string) bool { return owner == "" || owner == q.owner }
	filled := sdkmath.ZeroInt()
	n := 0
	for ; n < len(outs) && outs[n].fill; n++ {
		f := ents.OrderFills[outs[n].i]
		amount, ok := sdkmath.NewIntFromString(f.Amount)
		if !ok || f.Side != otherSide(q.side) || !samePrice(f.Price, q.price) || !same(f.Taker) || filled.Add(amount).GT(q.amount) {
			break
		}
		filled = filled.Add(amount)
	}
	saved := false
	if q.create && n < len(outs) && !outs[n].fill {
		o := ents.Orders[outs[n].i]
		amount, ok := sdkmath.NewIntFromString(o.Amount)
		saved = ok && o.Side == q.side && samePrice(o.Price, q.price) && same(o.Owner) && amount.Equal(q.amount.Sub(filled))
	}
	if n == 0 && !saved {
		return 0, false
	}
	// A chain v7.0 event names no owner or taker: the message's creator is
	// the one, or the order would belong to no account.
	for _, o := range outs[:n] {
		f := &ents.OrderFills[o.i]
		f.TakerTxHash = q.txHash
		if f.Taker == "" {
			f.Taker = q.owner
		}
	}
	if saved {
		ord := &ents.Orders[outs[n].i]
		ord.CreatedTxHash = q.txHash
		if ord.Owner == "" {
			ord.Owner, ord.OwnerFromMessage = q.owner, true
		}
		n++
	}
	return n, true
}
