-- Reverts 000025_price_source.
ALTER TABLE explorer.denoms DROP COLUMN IF EXISTS price_source;
