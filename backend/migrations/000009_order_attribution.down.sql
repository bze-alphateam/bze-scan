-- Reverts 000009_order_attribution.
DROP INDEX IF EXISTS explorer.idx_orders_open_owner;
DROP INDEX IF EXISTS explorer.idx_order_fills_unattributed;
DROP INDEX IF EXISTS explorer.idx_orders_unattributed;
