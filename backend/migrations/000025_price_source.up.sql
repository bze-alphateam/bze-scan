-- Where a denom's USD price comes from (BFE-312): the prices set now prices
-- every denom by the DEX price chain first (the USDC pool's spot price, else
-- the last trade, over the USDC and BZE routes) and by CoinGecko (the
-- chain-registry id) last. 'dex' | 'coingecko', NULL while unpriced. No
-- backfill: the next run of the set fills it.
ALTER TABLE explorer.denoms ADD COLUMN price_source TEXT
  CONSTRAINT denoms_price_source_check CHECK (price_source IN ('dex', 'coingecko'));
