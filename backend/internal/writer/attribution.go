package writer

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The transformer links an order book outcome to the transaction of the
// same height by message id (chain v8.2.0 and later). An outcome of a
// message processed in a later block than its transaction is linked here,
// after the flush that holds the second of the two rows, whichever comes
// first: on the live path the message row is older, the backfill writes the
// outcome first.
//
// The transaction is the order_messages row with the outcome's message id
// and the highest height at or below the outcome's: a newer message cannot
// carry the id while the older one is queued, and the transactions of the
// outcome's own block carry higher ids. The ids are reused as soon as the
// queue empties, almost every block, so the rule only holds once every
// height in between is indexed: a flush written out of order (the backfill
// writes in arrival order, a failed height is retried later) could
// otherwise hand the outcome to an older message with the same id. Each
// candidate therefore needs the blocks rows of every height from its
// message to itself, and the candidates are every unattributed outcome at
// or above the flush's lowest height: the outcomes of the flush, those of
// the messages it wrote, and those whose gap it filled.

// attributeOrdersSQL links the unattributed orders at or above $1 to the
// message that placed them.
const attributeOrdersSQL = `WITH c AS (
	SELECT o.order_id, m.tx_hash
	  FROM explorer.orders o
	 CROSS JOIN LATERAL (SELECT om.height, om.tx_hash, om.kind FROM explorer.order_messages om
	                      WHERE om.message_id = o.message_id AND om.height <= o.created_height
	                      ORDER BY om.height DESC LIMIT 1) m
	 WHERE o.message_id IS NOT NULL AND o.created_tx_hash IS NULL AND o.created_height >= $1
	   AND m.kind <> 'cancel'
	   AND (SELECT count(*) FROM explorer.blocks b WHERE b.height BETWEEN m.height AND o.created_height)
	       = o.created_height - m.height + 1
	 ORDER BY o.order_id
	   FOR UPDATE OF o)
	UPDATE explorer.orders o SET created_tx_hash = c.tx_hash, updated_at = now()
	  FROM c WHERE o.order_id = c.order_id`

// attributeFillsSQL links the unattributed fills at or above $1 to the
// message that took them.
const attributeFillsSQL = `WITH c AS (
	SELECT f.height, f.seq, m.tx_hash
	  FROM explorer.order_fills f
	 CROSS JOIN LATERAL (SELECT om.height, om.tx_hash, om.kind FROM explorer.order_messages om
	                      WHERE om.message_id = f.taker_message_id AND om.height <= f.height
	                      ORDER BY om.height DESC LIMIT 1) m
	 WHERE f.taker_message_id IS NOT NULL AND f.taker_tx_hash IS NULL AND f.height >= $1
	   AND m.kind <> 'cancel'
	   AND (SELECT count(*) FROM explorer.blocks b WHERE b.height BETWEEN m.height AND f.height)
	       = f.height - m.height + 1
	 ORDER BY f.height, f.seq
	   FOR UPDATE OF f)
	UPDATE explorer.order_fills f SET taker_tx_hash = c.tx_hash
	  FROM c WHERE f.height = c.height AND f.seq = c.seq`

// AttributeOrders is the PostFlush hook that links the order book outcomes
// processed in a later block than their transaction (see above). Both
// statements only fill NULL columns, so running it again changes nothing.
func AttributeOrders(ctx context.Context, tx pgx.Tx, f *Flush) error {
	if f == nil || f.Entities == nil || len(f.Entities.Blocks) == 0 {
		return nil
	}
	lo := f.Entities.Blocks[0].Height
	for _, b := range f.Entities.Blocks[1:] {
		lo = min(lo, b.Height)
	}
	if _, err := tx.Exec(ctx, attributeOrdersSQL, lo); err != nil {
		return fmt.Errorf("attribute orders: %w", err)
	}
	if _, err := tx.Exec(ctx, attributeFillsSQL, lo); err != nil {
		return fmt.Errorf("attribute fills: %w", err)
	}
	return nil
}
