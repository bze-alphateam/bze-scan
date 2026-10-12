-- Reverts 000017_order_cancel_lookup to the id-only index of 000007.
DROP INDEX IF EXISTS explorer.idx_block_events_order_cancel;
CREATE INDEX idx_block_events_order_cancel ON explorer.block_events ((attrs->>'id'))
  WHERE type = 'bze.tradebin.OrderCanceledEvent';
