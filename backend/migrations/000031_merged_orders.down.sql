-- Reverts 000031_merged_orders. Orders merged meanwhile keep the status
-- 'merged'; run a reindex of the heights after migrating up again.
CREATE INDEX idx_block_events_order_outcome ON explorer.block_events ((attrs->>'message_id'), height)
  WHERE type IN ('bze.tradebin.OrderMessageRefundedEvent', 'bze.tradebin.OrderCancelSkippedEvent',
                 'bze.tradebin.OrderMessageFailedEvent');
DROP INDEX explorer.idx_block_events_order_refund;
ALTER TABLE explorer.orders
  DROP COLUMN IF EXISTS added_amount,
  DROP COLUMN IF EXISTS merged_from;
