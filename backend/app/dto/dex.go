package dto

import (
	"fmt"
	"strconv"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// OrderStatuses are the status filters of an account's orders.
var OrderStatuses = []string{"open", "filled", "cancelled", "merged"}

// Order is an order that rested on the book: an item of GET
// /api/v1/accounts/{address}/orders and the head of GET /api/v1/orders/{id}.
// Amounts are in base units of the market's base denom; created_tx_hash is
// null when the placing transaction is not known (before chain v8.2.0, or
// placed by a message processed in a later block).
//
// Chain v8.3.0 merges an order placed at a price where its owner already
// rests one into it: the merged order rests under the new order's id with
// merged_from (the id it replaced) and added_amount (what it added to it),
// and the replaced order ends with the status merged and merged_into (the
// new id). All three are null otherwise.
type Order struct {
	OrderID       string     `json:"order_id"`
	MarketID      string     `json:"market_id"`
	Owner         string     `json:"owner"`
	Side          string     `json:"side"`
	Price         string     `json:"price"`
	Amount        string     `json:"amount"`
	FilledAmount  string     `json:"filled_amount"`
	Status        string     `json:"status"`
	CreatedHeight int64      `json:"created_height"`
	CreatedTime   time.Time  `json:"created_time"`
	CreatedTxHash *string    `json:"created_tx_hash"`
	MessageID     *string    `json:"message_id"`
	ClosedHeight  *int64     `json:"closed_height"`
	ClosedTime    *time.Time `json:"closed_time"`
	MergedFrom    *string    `json:"merged_from"`
	AddedAmount   *string    `json:"added_amount"`
	MergedInto    *string    `json:"merged_into"`
}

// Fill is a queue message filling (part of) a resting order: an item of
// GET /api/v1/accounts/{address}/fills. Side is the resting order's side;
// taker_tx_hash is null when the taker's transaction is not known.
type Fill struct {
	Height         int64     `json:"height"`
	Time           time.Time `json:"time"`
	OrderID        string    `json:"order_id"`
	MarketID       string    `json:"market_id"`
	Side           string    `json:"side"`
	Price          string    `json:"price"`
	Amount         string    `json:"amount"`
	Maker          string    `json:"maker"`
	Taker          string    `json:"taker"`
	TakerTxHash    *string   `json:"taker_tx_hash"`
	TakerMessageID *string   `json:"taker_message_id"`
	// TakerOrderID is the id of the taker's order (chain v8.3.0); absent
	// before.
	TakerOrderID *string `json:"taker_order_id,omitempty"`
}

// OrderDetail is GET /api/v1/orders/{id}: the order, its number of fills
// and the newest of them.
type OrderDetail struct {
	Order
	FillCount int64  `json:"fill_count"`
	Fills     []Fill `json:"fills"`
}

// TxDex is the order book outcome of a transaction on GET
// /api/v1/txs/{hash}: the orders it placed on the book, the fills it took,
// the orders it cancelled, the refunds, and pending when an order message it
// queued has no outcome indexed yet (the outcome may land in a later block).
//
// A refund is the part of an order that was not executed and went back to
// its owner (chain v8.3.0). skipped_cancels and failed belong to a design of
// chain v8.3.0 that no network ever ran: they are always empty arrays, kept
// for one release so clients do not break, and will be removed.
type TxDex struct {
	Placed         []Order    `json:"placed"`
	Fills          []Fill     `json:"fills"`
	Cancelled      []Order    `json:"cancelled"`
	Refunds        []Refund   `json:"refunds"`
	SkippedCancels []struct{} `json:"skipped_cancels"`
	Failed         []struct{} `json:"failed"`
	Pending        bool       `json:"pending"`
}

// Refund is the part of an order that was not executed: amount is the base
// amount, refund the coin sent back (null when it rounded to zero and only
// dust was kept), reason no_match | unfilled_remainder | below_min_amount |
// max_fills_reached | upgrade. A refund of a transaction of chain v8.3.0
// names the taker order in order_id and has no message_id; the upgrade
// block's refund of a transaction of chain v8.2.x names its queue message in
// both; a refund of a pre-release build has message_id only.
type Refund struct {
	Height    int64  `json:"height"`
	MessageID string `json:"message_id,omitempty"`
	OrderID   string `json:"order_id,omitempty"`
	MarketID  string `json:"market_id"`
	OrderType string `json:"order_type"`
	Amount    string `json:"amount"`
	Refund    *Coin  `json:"refund"`
	Reason    string `json:"reason"`
}

// NewOrder maps an orders row.
func NewOrder(o repository.Order) Order {
	return Order{
		OrderID: o.OrderID, MarketID: o.MarketID, Owner: o.Owner, Side: o.Side, Price: o.Price, Amount: o.Amount,
		FilledAmount: o.FilledAmount, Status: o.Status, CreatedHeight: o.CreatedHeight, CreatedTime: o.CreatedTime.UTC(),
		CreatedTxHash: o.CreatedTxHash, MessageID: o.MessageID, ClosedHeight: o.ClosedHeight, ClosedTime: utc(o.ClosedTime),
		MergedFrom: o.MergedFrom, AddedAmount: o.AddedAmount, MergedInto: o.MergedInto,
	}
}

// NewFill maps an order_fills row.
func NewFill(f repository.OrderFill) Fill {
	return Fill{
		Height: f.Height, Time: f.Time.UTC(), OrderID: f.OrderID, MarketID: f.MarketID, Side: f.Side, Price: f.Price,
		Amount: f.Amount, Maker: f.Maker, Taker: f.Taker, TakerTxHash: f.TakerTxHash, TakerMessageID: f.TakerMessageID,
		TakerOrderID: f.TakerOrderID,
	}
}

// NewOrderDetail maps the order page.
func NewOrderDetail(o *repository.Order, fillCount int64, fills []repository.OrderFill) OrderDetail {
	return OrderDetail{Order: NewOrder(*o), FillCount: fillCount, Fills: newFills(fills)}
}

// NewTxDex maps a transaction's order book outcome; nil when it has none
// and nothing pending (the transaction placed, filled and cancelled
// nothing).
func NewTxDex(d *repository.TxDex) *TxDex {
	if d == nil || (len(d.Placed) == 0 && len(d.Fills) == 0 && len(d.Cancelled) == 0 && len(d.Refunds) == 0 && !d.Pending) {
		return nil
	}
	out := &TxDex{Placed: newOrders(d.Placed), Fills: newFills(d.Fills), Cancelled: newOrders(d.Cancelled),
		Refunds: make([]Refund, 0, len(d.Refunds)), SkippedCancels: []struct{}{}, Failed: []struct{}{},
		Pending: d.Pending}
	for _, r := range d.Refunds {
		out.Refunds = append(out.Refunds, Refund{Height: r.Height, MessageID: r.MessageID, OrderID: r.OrderID,
			MarketID: r.MarketID, OrderType: r.OrderType, Amount: r.Amount, Refund: parseCoin(r.Refund), Reason: r.Reason})
	}
	return out
}

// parseCoin reads a coin string ("16087378657ubze"); nil when it is empty
// or not a coin.
func parseCoin(s string) *Coin {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i == len(s) {
		return nil
	}
	return &Coin{Denom: s[i:], Amount: s[:i]}
}

func newOrders(rows []repository.Order) []Order {
	out := make([]Order, 0, len(rows))
	for _, o := range rows {
		out = append(out, NewOrder(o))
	}
	return out
}

func newFills(rows []repository.OrderFill) []Fill {
	out := make([]Fill, 0, len(rows))
	for _, f := range rows {
		out = append(out, NewFill(f))
	}
	return out
}

// OrderIDDigits is the length of a tradebin order id.
const OrderIDDigits = 24

// NormaliseOrderID zero-fills an order id given as up to 24 digits; false
// for anything else.
func NormaliseOrderID(s string) (string, bool) {
	if s == "" || len(s) > OrderIDDigits {
		return "", false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return fmt.Sprintf("%0*s", OrderIDDigits, s), true
}

// EncodeOrderCursor is the cursor after an order of an owner's list: its
// created height and its order id as a number.
func EncodeOrderCursor(o repository.Order) string {
	id, _ := strconv.ParseInt(o.OrderID, 10, 64) // the chain's counter: 24 digits, far below 2^63
	return EncodeCursor(o.CreatedHeight, id)
}

// DecodeOrderCursor reverses EncodeOrderCursor.
func DecodeOrderCursor(cursor string) (*repository.OrderKey, error) {
	keys, err := DecodeCursor(cursor, 2)
	if err != nil {
		return nil, err
	}
	return &repository.OrderKey{Height: keys[0], OrderID: fmt.Sprintf("%0*d", OrderIDDigits, keys[1])}, nil
}

// EncodeFillCursor is the cursor after a fill: its height and seq.
func EncodeFillCursor(f repository.OrderFill) string {
	return EncodeCursor(f.Height, f.Seq)
}

// DecodeFillCursor reverses EncodeFillCursor.
func DecodeFillCursor(cursor string) (*repository.FillKey, error) {
	keys, err := DecodeCursor(cursor, 2)
	if err != nil {
		return nil, err
	}
	return &repository.FillKey{Height: keys[0], Seq: keys[1]}, nil
}
