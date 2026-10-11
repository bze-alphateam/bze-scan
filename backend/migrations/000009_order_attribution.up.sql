-- Lookups of the order attribution and the order reconciliation (BFE-101).
--
-- The outcomes the attribution query still has to link to their
-- transaction: few rows (an outcome of a message processed in a later block
-- than its transaction, until the blocks in between are indexed), read
-- after every flush.
CREATE INDEX idx_orders_unattributed ON explorer.orders (created_height)
  WHERE message_id IS NOT NULL AND created_tx_hash IS NULL;
CREATE INDEX idx_order_fills_unattributed ON explorer.order_fills (height)
  WHERE taker_message_id IS NOT NULL AND taker_tx_hash IS NULL;
-- The open orders per owner, which the hourly reconciliation compares with
-- the node's order book.
CREATE INDEX idx_orders_open_owner ON explorer.orders (owner) WHERE status = 'open';
