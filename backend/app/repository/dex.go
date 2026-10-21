package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Order is an orders row.
type Order struct {
	OrderID       string
	MarketID      string
	Owner         string
	Side          string
	Price         string
	Amount        string
	FilledAmount  string
	Status        string
	CreatedHeight int64
	CreatedTime   time.Time
	CreatedTxHash *string
	MessageID     *string
	ClosedHeight  *int64
	ClosedTime    *time.Time
}

// OrderFill is an order_fills row.
type OrderFill struct {
	Height         int64
	Seq            int64
	OrderID        string
	MarketID       string
	Side           string
	Price          string
	Amount         string
	Maker          string
	Taker          string
	TakerMessageID *string
	TakerTxHash    *string
	Time           time.Time
}

// OrderKey is the keyset position of an order in an owner's list.
type OrderKey struct {
	Height  int64
	OrderID string
}

// FillKey is the keyset position of a fill.
type FillKey struct {
	Height int64
	Seq    int64
}

// TxDex is the order book outcome of one transaction: the orders it placed,
// the fills it took, the orders it cancelled, what was sent back, the
// cancels that found no order, the messages the engine failed to process,
// and whether an order message it queued has no outcome indexed yet.
type TxDex struct {
	Placed         []Order
	Fills          []OrderFill
	Cancelled      []Order
	Refunds        []OrderRefund
	SkippedCancels []CancelSkip
	Failed         []OrderFailure
	Pending        bool
}

// OrderRefund is an OrderMessageRefundedEvent (chain v8.3.0): the part of a
// queue message that was not executed went back to its owner.
type OrderRefund struct {
	Height    int64
	MessageID string
	MarketID  string
	OrderType string
	Owner     string
	// Amount is the base amount not executed.
	Amount string
	// Refund is the coin sent back as the chain writes it; empty when it
	// rounded to zero and only dust was stored.
	Refund string
	Reason string
}

// CancelSkip is an OrderCancelSkippedEvent (chain v8.3.0): a cancel found
// no order, which was filled or cancelled meanwhile.
type CancelSkip struct {
	Height    int64
	MessageID string
	MarketID  string
	OrderType string
	OrderID   string
	Owner     string
	Reason    string
}

// OrderFailure is the latest OrderMessageFailedEvent of a queue message
// (chain v8.3.0): the engine could not process it at Height; it stays
// queued and is retried every block.
type OrderFailure struct {
	MessageID   string
	MessageType string
	Height      int64
}

const (
	orderCols = `o.order_id, o.market_id, o.owner, o.side, o.price::text, o.amount::text, o.filled_amount::text,
		o.status, o.created_height, o.created_time, o.created_tx_hash, o.message_id, o.closed_height, o.closed_time`
	fillCols = `f.height, f.seq, f.order_id, f.market_id, f.side, f.price::text, f.amount::text, f.maker, f.taker,
		f.taker_message_id, f.taker_tx_hash, f.time`
)

func scanOrder(row pgx.CollectableRow) (Order, error) {
	var o Order
	err := row.Scan(&o.OrderID, &o.MarketID, &o.Owner, &o.Side, &o.Price, &o.Amount, &o.FilledAmount, &o.Status,
		&o.CreatedHeight, &o.CreatedTime, &o.CreatedTxHash, &o.MessageID, &o.ClosedHeight, &o.ClosedTime)
	return o, err
}

func scanFill(row pgx.CollectableRow) (OrderFill, error) {
	var f OrderFill
	err := row.Scan(&f.Height, &f.Seq, &f.OrderID, &f.MarketID, &f.Side, &f.Price, &f.Amount, &f.Maker, &f.Taker,
		&f.TakerMessageID, &f.TakerTxHash, &f.Time)
	return f, err
}

func (r *Explorer) orders(ctx context.Context, what, sql string, args ...any) ([]Order, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	out, err := pgx.CollectRows(rows, scanOrder)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}

func (r *Explorer) fills(ctx context.Context, what, sql string, args ...any) ([]OrderFill, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	out, err := pgx.CollectRows(rows, scanFill)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}

// collect runs sql and scans every row with scan.
func collect[T any](ctx context.Context, db DB, what string, scan pgx.RowToFunc[T], sql string, args ...any) ([]T, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	out, err := pgx.CollectRows(rows, scan)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}

// AccountOrders returns up to limit orders of owner, newest first (created
// height, then order id, descending), only of status when it is not nil,
// after before when it is not nil.
func (r *Explorer) AccountOrders(ctx context.Context, owner string, status *string, before *OrderKey,
	limit int) ([]Order, error) {
	var beforeHeight *int64
	var beforeID *string
	if before != nil {
		beforeHeight, beforeID = &before.Height, &before.OrderID
	}
	return r.orders(ctx, "orders of "+owner, `SELECT `+orderCols+` FROM explorer.orders o
		WHERE o.owner = $1
		  AND ($2::text IS NULL OR o.status = $2)
		  AND ($3::bigint IS NULL OR (o.created_height, o.order_id) < ($3::bigint, $4::text))
		ORDER BY o.created_height DESC, o.order_id DESC
		LIMIT $5`, owner, status, beforeHeight, beforeID, limit)
}

// AccountFills returns up to limit fills where address is the maker or the
// taker, newest first (height, then seq, descending), after before when it
// is not nil. Each side reads its own index.
func (r *Explorer) AccountFills(ctx context.Context, address string, before *FillKey, limit int) ([]OrderFill, error) {
	var beforeHeight, beforeSeq *int64
	if before != nil {
		beforeHeight, beforeSeq = &before.Height, &before.Seq
	}
	side := func(col string) string {
		return `(SELECT ` + fillCols + ` FROM explorer.order_fills f
			WHERE f.` + col + ` = $1 AND ($2::bigint IS NULL OR (f.height, f.seq) < ($2::bigint, $3::integer))
			ORDER BY f.height DESC, f.seq DESC LIMIT $4)`
	}
	return r.fills(ctx, "fills of "+address, `SELECT * FROM (`+side("maker")+` UNION `+side("taker")+`) u
		ORDER BY height DESC, seq DESC LIMIT $4`, address, beforeHeight, beforeSeq, limit)
}

// Order returns the order with id, or ErrNotFound.
func (r *Explorer) Order(ctx context.Context, id string) (*Order, error) {
	out, err := r.orders(ctx, "order "+id, `SELECT `+orderCols+` FROM explorer.orders o WHERE o.order_id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}

// OrderFills returns the number of fills of the order with id and the
// newest limit of them, newest first.
func (r *Explorer) OrderFills(ctx context.Context, id string, limit int) (int64, []OrderFill, error) {
	var count int64
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM explorer.order_fills WHERE order_id = $1`, id).
		Scan(&count); err != nil {
		return 0, nil, fmt.Errorf("fills of order %s: %w", id, err)
	}
	fills, err := r.fills(ctx, "fills of order "+id, `SELECT `+fillCols+` FROM explorer.order_fills f
		WHERE f.order_id = $1 ORDER BY f.height DESC, f.seq DESC LIMIT $2`, id, limit)
	return count, fills, err
}

// cancelledIDs are the order ids a transaction's cancel message events name.
const cancelledIDs = `SELECT e->'attrs'->>'orderId' FROM explorer.messages m, jsonb_array_elements(m.events) e
	WHERE m.height = $1 AND m.tx_index = $2 AND e->>'type' = 'bze.tradebin.OrderCancelMessageEvent'`

// Since chain v8.3.0 the engine also reports the queue messages that end
// without an order or a fill. Those events are only kept as block_events
// rows; one belongs to an order message of the transaction (at $1, $2)
// when it carries its message id at or above its height and no later
// order message took the id before it: the ids are reused once the queue
// empties, never while a message holding one is queued (see
// writer.AttributeOrders). A failure repeats every block the message stays
// queued; the read keeps the latest.
const (
	evRefunded      = "bze.tradebin.OrderMessageRefundedEvent"
	evCancelSkipped = "bze.tradebin.OrderCancelSkippedEvent"
	evFailed        = "bze.tradebin.OrderMessageFailedEvent"
)

// txOutcomeEvents selects the events of type $3 of the transaction's order
// messages; a query adds its own columns and ordering.
const txOutcomeEvents = ` FROM explorer.order_messages om
	  JOIN explorer.block_events e ON e.type = $3 AND e.attrs->>'message_id' = om.message_id AND e.height >= $1
	 WHERE om.height = $1 AND om.tx_index = $2
	   AND NOT EXISTS (SELECT 1 FROM explorer.order_messages n
	                    WHERE n.message_id = om.message_id AND n.height > om.height AND n.height <= e.height)`

// settledByEvent is true when the order message om has a refund or a
// skipped cancel (chain v8.3.0).
const settledByEvent = `EXISTS (SELECT 1 FROM explorer.block_events e
	 WHERE e.type IN ('` + evRefunded + `', '` + evCancelSkipped + `')
	   AND e.attrs->>'message_id' = om.message_id AND e.height >= om.height
	   AND NOT EXISTS (SELECT 1 FROM explorer.order_messages n
	                    WHERE n.message_id = om.message_id AND n.height > om.height AND n.height <= e.height))`

// txPendingSQL is true when an order message of the transaction has no
// outcome indexed: a create or fill message (chain v8.2.0 and later) with
// no fill, no order and no refund attributed to it, a cancel whose order is
// still open, or (chain v8.3.0) a cancel with neither its cancel nor a
// skip. A failure is no outcome: the message is retried.
const txPendingSQL = `SELECT EXISTS (
		SELECT 1 FROM explorer.order_messages om
		 WHERE om.height = $1 AND om.tx_index = $2 AND om.kind <> 'cancel'
		   AND NOT EXISTS (SELECT 1 FROM explorer.order_fills f
		                    WHERE f.taker_tx_hash = $3 AND f.taker_message_id = om.message_id AND f.height >= om.height)
		   AND NOT EXISTS (SELECT 1 FROM explorer.orders o
		                    WHERE o.created_tx_hash = $3 AND o.message_id = om.message_id)
		   AND NOT ` + settledByEvent + `)
	OR EXISTS (SELECT 1 FROM explorer.orders o WHERE o.status = 'open' AND o.order_id IN (` + cancelledIDs + `))`

// txRefundsSQL, txSkippedCancelsSQL and txFailuresSQL read the
// transaction's v8.3.0 outcome events.
const (
	txRefundsSQL = `SELECT e.height, om.message_id, coalesce(e.attrs->>'market_id', ''),
		coalesce(e.attrs->>'order_type', ''), coalesce(e.attrs->>'owner', ''), coalesce(e.attrs->>'amount', ''),
		coalesce(e.attrs->>'refund', ''), coalesce(e.attrs->>'reason', '')` + txOutcomeEvents + `
	 ORDER BY e.height, e.seq`
	txSkippedCancelsSQL = `SELECT e.height, om.message_id, coalesce(e.attrs->>'market_id', ''),
		coalesce(e.attrs->>'order_type', ''), coalesce(e.attrs->>'order_id', ''), coalesce(e.attrs->>'owner', ''),
		coalesce(e.attrs->>'reason', '')` + txOutcomeEvents + `
	 ORDER BY e.height, e.seq`
	txFailuresSQL = `SELECT DISTINCT ON (om.message_id) om.message_id, coalesce(e.attrs->>'message_type', ''), e.height` +
		txOutcomeEvents + `
	 ORDER BY om.message_id, e.height DESC`
)

// TxDex returns the order book outcome of the transaction with hash at
// (height, txIndex).
func (r *Explorer) TxDex(ctx context.Context, height, txIndex int64, hash string) (*TxDex, error) {
	var out TxDex
	var err error
	if out.Placed, err = r.orders(ctx, "orders placed by "+hash, `SELECT `+orderCols+` FROM explorer.orders o
		WHERE o.created_tx_hash = $1 ORDER BY o.order_id`, hash); err != nil {
		return nil, err
	}
	if out.Fills, err = r.fills(ctx, "fills taken by "+hash, `SELECT `+fillCols+` FROM explorer.order_fills f
		WHERE f.taker_tx_hash = $1 AND f.height >= $2 ORDER BY f.height, f.seq`, hash, height); err != nil {
		return nil, err
	}
	if out.Cancelled, err = r.orders(ctx, "orders cancelled by "+hash, `SELECT `+orderCols+` FROM explorer.orders o
		WHERE o.status = 'cancelled' AND o.order_id IN (`+cancelledIDs+`) ORDER BY o.order_id`,
		height, txIndex); err != nil {
		return nil, err
	}
	if out.Refunds, err = collect(ctx, r.db, "refunds of "+hash, func(row pgx.CollectableRow) (OrderRefund, error) {
		var x OrderRefund
		err := row.Scan(&x.Height, &x.MessageID, &x.MarketID, &x.OrderType, &x.Owner, &x.Amount, &x.Refund, &x.Reason)
		return x, err
	}, txRefundsSQL, height, txIndex, evRefunded); err != nil {
		return nil, err
	}
	if out.SkippedCancels, err = collect(ctx, r.db, "skipped cancels of "+hash, func(row pgx.CollectableRow) (CancelSkip, error) {
		var x CancelSkip
		err := row.Scan(&x.Height, &x.MessageID, &x.MarketID, &x.OrderType, &x.OrderID, &x.Owner, &x.Reason)
		return x, err
	}, txSkippedCancelsSQL, height, txIndex, evCancelSkipped); err != nil {
		return nil, err
	}
	if out.Failed, err = collect(ctx, r.db, "failed messages of "+hash, func(row pgx.CollectableRow) (OrderFailure, error) {
		var x OrderFailure
		err := row.Scan(&x.MessageID, &x.MessageType, &x.Height)
		return x, err
	}, txFailuresSQL, height, txIndex, evFailed); err != nil {
		return nil, err
	}
	err = r.db.QueryRow(ctx, txPendingSQL, height, txIndex, hash).Scan(&out.Pending)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("pending orders of %s: %w", hash, err)
	}
	return &out, nil
}
