-- Lookups of the DEX endpoints and writers (BFE-98).
--
-- Fills where an address is the maker (the account fills list reads the
-- taker index and this one), the outcome of a transaction (orders it placed,
-- fills it took), and the cancel of an order, which the orders insert reads
-- so an order written after its cancel (the backfill walks down) is closed.
CREATE INDEX idx_order_fills_maker    ON explorer.order_fills (maker, height DESC);
CREATE INDEX idx_order_fills_taker_tx ON explorer.order_fills (taker_tx_hash) WHERE taker_tx_hash IS NOT NULL;
CREATE INDEX idx_orders_created_tx    ON explorer.orders (created_tx_hash) WHERE created_tx_hash IS NOT NULL;
CREATE INDEX idx_block_events_order_cancel ON explorer.block_events ((attrs->>'id'))
  WHERE type = 'bze.tradebin.OrderCanceledEvent';
