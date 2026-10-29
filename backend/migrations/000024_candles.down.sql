-- Reverts 000024_candles.
DELETE FROM explorer.pending_rewrites WHERE name = 'candles' OR starts_with(name, 'candles:');
DROP INDEX IF EXISTS explorer.idx_swaps_pool_time;
DROP INDEX IF EXISTS explorer.idx_order_fills_market_time;
DROP TABLE IF EXISTS explorer.candles;
