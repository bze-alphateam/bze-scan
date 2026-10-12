-- The orders insert reads an order's newest cancel. On the id alone the
-- planner preferred walking each partition's primary key backwards for the
-- newest height, once per order; with the height in the index the lookup is
-- one index probe.
DROP INDEX IF EXISTS explorer.idx_block_events_order_cancel;
CREATE INDEX idx_block_events_order_cancel ON explorer.block_events ((attrs->>'id'), height DESC)
  WHERE type = 'bze.tradebin.OrderCanceledEvent';
