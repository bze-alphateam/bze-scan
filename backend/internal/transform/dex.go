package transform

import (
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// The tradebin order book works in two steps: a transaction only enqueues a
// message (and emits OrderCreateMessageEvent or OrderCancelMessageEvent), the
// engine processes the queue at EndBlock, normally of the same block, and
// emits the outcome as block-level events. Since chain release v8.2.0 every
// one of these events carries the queue message id, which links a
// transaction to its outcome.

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
}

// OrderCancel is an OrderCanceledEvent: the order is closed as cancelled at
// Height.
type OrderCancel struct {
	OrderID string    `json:"order_id"`
	Height  int64     `json:"height"`
	Time    time.Time `json:"time"`
}

// blockOrders appends the orders, fills and cancels of the block's finalize
// events.
func blockOrders(ents *Entities, b Block, events []node.Event) {
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
			ents.Orders = append(ents.Orders, Order{
				OrderID: get("id"), MarketID: get("market_id"), Owner: get("owner"), Side: get("order_type"),
				Price: get("price"), Amount: get("amount"), Height: b.Height, Time: b.Time,
				MessageID: get("message_id"),
			})
		case evOrderExecuted:
			ents.OrderFills = append(ents.OrderFills, OrderFill{
				Height: b.Height, Seq: i, OrderID: get("id"), MarketID: get("market_id"), Side: get("order_type"),
				Price: get("price"), Amount: get("amount"), Maker: get("maker"), Taker: get("taker"),
				TakerMessageID: get("message_id"), Time: b.Time,
			})
		case evOrderCanceled:
			ents.OrderCancels = append(ents.OrderCancels, OrderCancel{OrderID: get("id"), Height: b.Height, Time: b.Time})
		}
	}
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
