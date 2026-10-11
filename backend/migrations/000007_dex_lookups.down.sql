-- Reverts 000007_dex_lookups.
DROP INDEX IF EXISTS explorer.idx_block_events_order_cancel;
DROP INDEX IF EXISTS explorer.idx_orders_created_tx;
DROP INDEX IF EXISTS explorer.idx_order_fills_taker_tx;
DROP INDEX IF EXISTS explorer.idx_order_fills_maker;
