package writer

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/jackc/pgx/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// The order book writes. order_messages and order_fills are history rows
// written like the others; orders is one row per order, inserted from its
// OrderSavedEvent and then moved by its fills and its cancel. The backfill
// walks down, so an order's fills and cancel can be written before the
// order: the orders insert reads them, and a fill or a cancel that finds no
// order row changes nothing.

// FillKey is the natural key of an order_fills row.
type FillKey struct {
	Height int64
	Seq    int
}

var (
	orderMessagesTable = table{
		name: "explorer.order_messages",
		keys: []string{"height", "message_id"},
		cols: []column{
			{name: "height", typ: "bigint"},
			{name: "message_id", typ: "text"},
			{name: "tx_index", typ: "integer"},
			{name: "msg_index", typ: "integer"},
			{name: "tx_hash", typ: "text"},
			{name: "kind", typ: "text"},
		},
	}
	orderFillsTable = table{
		name: "explorer.order_fills",
		keys: []string{"height", "seq"},
		cols: []column{
			{name: "height", typ: "bigint"},
			{name: "seq", typ: "integer"},
			{name: "order_id", typ: "text"},
			{name: "market_id", typ: "text"},
			{name: "side", typ: "text"},
			{name: "price", typ: "numeric"},
			{name: "amount", typ: "numeric"},
			{name: "maker", typ: "text"},
			{name: "taker", typ: "text"},
			{name: "taker_message_id", typ: "text"},
			{name: "taker_tx_hash", typ: "text", expr: fillTakerTxHash},
			{name: "time", typ: "timestamptz"},
		},
	}
)

// fillTakerTxHash keeps the taker transaction a fill row already holds when
// the transformer found none in the fill's block (a reindex never forgets a
// link made after the write).
const fillTakerTxHash = `COALESCE(r.taker_tx_hash,
		(SELECT f.taker_tx_hash FROM explorer.order_fills f WHERE f.height = r.height AND f.seq = r.seq))`

// orderMessageRow, orderRow, orderFillRow, fillDeltaRow and orderCancelRow
// are the JSON shapes of the order book writes.
type (
	orderMessageRow struct {
		Height    int64  `json:"height"`
		MessageID string `json:"message_id"`
		TxIndex   int    `json:"tx_index"`
		MsgIndex  int    `json:"msg_index"`
		TxHash    string `json:"tx_hash"`
		Kind      string `json:"kind"`
	}
	orderRow struct {
		OrderID       string    `json:"order_id"`
		MarketID      string    `json:"market_id"`
		Owner         string    `json:"owner"`
		Side          string    `json:"side"`
		Price         string    `json:"price"`
		Amount        string    `json:"amount"`
		Height        int64     `json:"height"`
		Time          time.Time `json:"time"`
		MessageID     *string   `json:"message_id"`
		CreatedTxHash *string   `json:"created_tx_hash"`
	}
	orderFillRow struct {
		Height         int64     `json:"height"`
		Seq            int       `json:"seq"`
		OrderID        string    `json:"order_id"`
		MarketID       string    `json:"market_id"`
		Side           string    `json:"side"`
		Price          string    `json:"price"`
		Amount         string    `json:"amount"`
		Maker          string    `json:"maker"`
		Taker          string    `json:"taker"`
		TakerMessageID *string   `json:"taker_message_id"`
		TakerTxHash    *string   `json:"taker_tx_hash"`
		Time           time.Time `json:"time"`
	}
	// fillDeltaRow is what the fills a flush inserted add to one order: the
	// amount, and the last of them, which closes the order once it is
	// filled.
	fillDeltaRow struct {
		OrderID string    `json:"order_id"`
		Amount  string    `json:"amount"`
		Height  int64     `json:"height"`
		Time    time.Time `json:"time"`
	}
	orderCancelRow struct {
		OrderID string    `json:"order_id"`
		Height  int64     `json:"height"`
		Time    time.Time `json:"time"`
	}
)

// orderRecordset reads orderRow payloads.
const orderRecordset = `jsonb_to_recordset($1::jsonb) AS r(order_id text, market_id text, owner text, side text,
		price numeric, amount numeric, height bigint, time timestamptz, message_id text, created_tx_hash text)`

// ordersSQL inserts the saved orders. The fills and the cancel already
// stored (the backfill writes them first) set filled_amount, the status and
// closed_*; the closing block's time is unknown until its blocks row is
// written, and the cancel of the same flush, applied after this insert, sets
// it. The chain never saves an order id twice.
const ordersSQL = `INSERT INTO explorer.orders (order_id, market_id, owner, side, price, amount, filled_amount, status,
		created_height, created_time, created_tx_hash, message_id, closed_height, closed_time, updated_at)
	SELECT r.order_id, r.market_id, r.owner, r.side, r.price, r.amount, s.filled,
		CASE WHEN c.height IS NOT NULL THEN 'cancelled' WHEN s.filled >= r.amount THEN 'filled' ELSE 'open' END,
		r.height, r.time, r.created_tx_hash, r.message_id,
		CASE WHEN c.height IS NOT NULL THEN c.height WHEN s.filled >= r.amount THEN lf.height END,
		CASE WHEN c.height IS NOT NULL THEN c.time WHEN s.filled >= r.amount THEN lf.time END,
		now()
	  FROM ` + orderRecordset + `
	 CROSS JOIN LATERAL (SELECT coalesce(sum(f.amount), 0) AS filled
	                       FROM explorer.order_fills f WHERE f.order_id = r.order_id) s
	  LEFT JOIN LATERAL (SELECT f.height, f.time FROM explorer.order_fills f
	                      WHERE f.order_id = r.order_id ORDER BY f.height DESC, f.seq DESC LIMIT 1) lf ON true
	  LEFT JOIN LATERAL (SELECT e.height, b.time FROM explorer.block_events e
	                       LEFT JOIN explorer.blocks b ON b.height = e.height
	                      WHERE e.type = 'bze.tradebin.OrderCanceledEvent' AND e.attrs->>'id' = r.order_id
	                      ORDER BY e.height DESC LIMIT 1) c ON true
	 ORDER BY r.order_id
	ON CONFLICT (order_id) DO NOTHING`

// ordersUpdateSQL rewrites, for the reindex, the columns of a stored order
// that its OrderSavedEvent gives; the placing transaction is kept when the
// transformer found none. filled_amount, the status and closed_* belong to
// the fills and the cancel.
const ordersUpdateSQL = `UPDATE explorer.orders AS o SET
		market_id = r.market_id, owner = r.owner, side = r.side, price = r.price, amount = r.amount,
		created_height = r.height, created_time = r.time, message_id = r.message_id,
		created_tx_hash = COALESCE(r.created_tx_hash, o.created_tx_hash), updated_at = now()
	  FROM ` + orderRecordset + `
	 WHERE o.order_id = r.order_id
	   AND (o.market_id, o.owner, o.side, o.price, o.amount, o.created_height, o.created_time, o.message_id,
	        o.created_tx_hash) IS DISTINCT FROM
	       (r.market_id, r.owner, r.side, r.price, r.amount, r.height, r.time, r.message_id,
	        COALESCE(r.created_tx_hash, o.created_tx_hash))`

// orderCancelsSQL closes cancelled orders, locking them in order id order.
const orderCancelsSQL = `WITH r AS (SELECT * FROM jsonb_to_recordset($1::jsonb) AS r(order_id text, height bigint, time timestamptz)),
	locked AS (SELECT o.order_id FROM explorer.orders o JOIN r ON r.order_id = o.order_id
	            ORDER BY o.order_id FOR UPDATE OF o)
	UPDATE explorer.orders AS o SET status = 'cancelled', closed_height = r.height, closed_time = r.time,
		updated_at = now()
	  FROM r
	 WHERE o.order_id = r.order_id AND o.order_id IN (SELECT order_id FROM locked)
	   AND (o.status, o.closed_height, o.closed_time) IS DISTINCT FROM ('cancelled', r.height, r.time)`

// fillDeltasSQL adds the fills a flush inserted to their orders, locking
// them in order id order: an open order whose fills reach its amount is
// filled and closed at its last fill.
const fillDeltasSQL = `WITH d AS (SELECT * FROM jsonb_to_recordset($1::jsonb) AS r(order_id text, amount numeric, height bigint,
		time timestamptz)),
	locked AS (SELECT o.order_id FROM explorer.orders o JOIN d ON d.order_id = o.order_id
	            ORDER BY o.order_id FOR UPDATE OF o)
	UPDATE explorer.orders AS o SET
		filled_amount = o.filled_amount + d.amount,
		status = CASE WHEN o.status = 'open' AND o.filled_amount + d.amount >= o.amount THEN 'filled' ELSE o.status END,
		closed_height = CASE WHEN o.status <> 'cancelled' AND o.filled_amount + d.amount >= o.amount
		                      AND (o.closed_height IS NULL OR d.height > o.closed_height)
		                     THEN d.height ELSE o.closed_height END,
		closed_time = CASE WHEN o.status <> 'cancelled' AND o.filled_amount + d.amount >= o.amount
		                    AND (o.closed_height IS NULL OR d.height > o.closed_height)
		                   THEN d.time ELSE o.closed_time END,
		updated_at = now()
	  FROM d
	 WHERE o.order_id = d.order_id AND o.order_id IN (SELECT order_id FROM locked)`

// dexStatements returns the order book writes of ents that do not depend on
// the rows inserted: the order messages, the orders (inserted, and in
// ModeUpdate rewritten), the fills, then the cancels.
func dexStatements(top int64, ents *transform.Entities, mode Mode) ([]statement, error) {
	messages := make([]orderMessageRow, 0, len(ents.OrderMessages))
	for _, m := range ents.OrderMessages {
		messages = append(messages, orderMessageRow{
			Height: m.Height, MessageID: m.MessageID, TxIndex: m.TxIndex, MsgIndex: m.MsgIndex, TxHash: m.TxHash, Kind: m.Kind,
		})
	}
	orders := make([]orderRow, 0, len(ents.Orders))
	for _, o := range ents.Orders {
		orders = append(orders, orderRow{
			OrderID: o.OrderID, MarketID: o.MarketID, Owner: o.Owner, Side: o.Side, Price: o.Price, Amount: o.Amount,
			Height: o.Height, Time: o.Time, MessageID: nullIfEmpty(o.MessageID), CreatedTxHash: nullIfEmpty(o.CreatedTxHash),
		})
	}
	slices.SortFunc(orders, func(a, b orderRow) int { return cmp.Compare(a.OrderID, b.OrderID) })
	fills := make([]orderFillRow, 0, len(ents.OrderFills))
	for _, f := range ents.OrderFills {
		fills = append(fills, orderFillRow{
			Height: f.Height, Seq: f.Seq, OrderID: f.OrderID, MarketID: f.MarketID, Side: f.Side, Price: f.Price,
			Amount: f.Amount, Maker: f.Maker, Taker: f.Taker, TakerMessageID: nullIfEmpty(f.TakerMessageID),
			TakerTxHash: nullIfEmpty(f.TakerTxHash), Time: f.Time,
		})
	}
	// One cancel per order: the chain cancels an order once.
	cancels := mergeDeltas(ents.OrderCancels, func(c transform.OrderCancel) string { return c.OrderID },
		func(a, b transform.OrderCancel) transform.OrderCancel {
			if b.Height > a.Height {
				return b
			}
			return a
		})
	cancelRows := make([]orderCancelRow, 0, len(cancels))
	for _, c := range cancels {
		cancelRows = append(cancelRows, orderCancelRow{OrderID: c.OrderID, Height: c.Height, Time: c.Time})
	}

	msgs, err := chunked(top, "order_messages", orderMessagesTable, mode, messages, nil)
	if err != nil {
		return nil, err
	}
	var ords []statement
	if mode == ModeUpdate {
		if ords, err = plainStatements(top, "orders update", ordersUpdateSQL, orders); err != nil {
			return nil, err
		}
	}
	inserts, err := plainStatements(top, "orders", ordersSQL, orders)
	if err != nil {
		return nil, err
	}
	fillStmts, err := chunked(top, "order_fills", orderFillsTable, mode, fills, scanFillKeys)
	if err != nil {
		return nil, err
	}
	cancelStmts, err := plainStatements(top, "order cancels", orderCancelsSQL, cancelRows)
	if err != nil {
		return nil, err
	}
	return slices.Concat(msgs, ords, inserts, fillStmts, cancelStmts), nil
}

// fillDeltas merges the fills of ents that were inserted per order, in
// order id order.
func fillDeltas(ents *transform.Entities, inserted Inserted) ([]fillDeltaRow, error) {
	type delta struct {
		orderID string
		amount  sdkmath.Int
		height  int64
		time    time.Time
	}
	var deltas []delta
	for _, f := range ents.OrderFills {
		if !inserted.OrderFills[FillKey{Height: f.Height, Seq: f.Seq}] {
			continue
		}
		amount, ok := sdkmath.NewIntFromString(f.Amount)
		if !ok {
			return nil, fmt.Errorf("fill %d/%d: amount %q is not an integer", f.Height, f.Seq, f.Amount)
		}
		deltas = append(deltas, delta{orderID: f.OrderID, amount: amount, height: f.Height, time: f.Time})
	}
	merged := mergeDeltas(deltas, func(d delta) string { return d.orderID }, func(a, b delta) delta {
		a.amount = a.amount.Add(b.amount)
		if b.height > a.height {
			a.height, a.time = b.height, b.time
		}
		return a
	})
	out := make([]fillDeltaRow, len(merged))
	for i, d := range merged {
		out[i] = fillDeltaRow{OrderID: d.orderID, Amount: d.amount.String(), Height: d.height, Time: d.time}
	}
	return out, nil
}

// fillStatements returns the filled_amount writes of a flush whose inserted
// fills are known.
func fillStatements(top int64, ents *transform.Entities, inserted Inserted) ([]statement, error) {
	deltas, err := fillDeltas(ents, inserted)
	if err != nil {
		return nil, fmt.Errorf("write %d: %w", top, err)
	}
	return plainStatements(top, "order fill deltas", fillDeltasSQL, deltas)
}

func scanFillKeys(rows pgx.Rows, into *Inserted) error {
	for rows.Next() {
		var k FillKey
		if err := rows.Scan(&k.Height, &k.Seq); err != nil {
			return err
		}
		into.OrderFills[k] = true
	}
	return rows.Err()
}
