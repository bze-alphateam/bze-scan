-- Merged orders and the upgrade-block refunds of chain v8.3.0 (BFE-347).
--
-- An order placed at a price where its owner already rests one is merged into
-- it: the merged order rests under the new order's id (OrderAmendedEvent) and
-- the resting order is gone. The new order records the id it replaced and the
-- amount it added; the replaced order ends with the status 'merged'
-- (explorer.orders.status: open | filled | cancelled | merged; no constraint
-- holds the list) and an order_closures row naming the order it merged into.
ALTER TABLE explorer.orders
  ADD COLUMN merged_from  TEXT,
  ADD COLUMN added_amount NUMERIC(78,0);

-- The refunds a transaction reads from the block events: the ones keyed by a
-- queue message id (chain v8.3.0 pre-release builds) and the upgrade block's,
-- which name the old queue message in order_id. Replaces
-- idx_block_events_order_outcome (migration 000022), which also served the
-- skipped-cancel and failure events that no network ever emitted.
CREATE INDEX idx_block_events_order_refund
  ON explorer.block_events ((COALESCE(attrs->>'message_id', attrs->>'order_id')), height)
  WHERE type = 'bze.tradebin.OrderMessageRefundedEvent';
DROP INDEX explorer.idx_block_events_order_outcome;
