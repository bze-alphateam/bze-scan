-- OHLC candles of the DEX's trades (BFE-303), for the aggregator-compatible
-- GET /aggregator/api/dex/intervals.
--
-- One row per market (an order book market id, base/quote, or a pool id,
-- base_quote), length in minutes (5, 15, 60, 240, 1440) and bucket start,
-- the buckets aligned to the Unix epoch. Chain units: prices are quote base
-- units per base base unit, volumes base units; the display conversion is
-- done on read. The average price is derived on read from the volumes (or
-- the close when high = low). internal/candles writes every row, in Go:
-- the writers' PostFlush hook and internal/trades recompute the UTC days of
-- the trades they write from the trades themselves, `bze-scan candles
-- rebuild` and the post-migration step "candles" whole markets.
CREATE TABLE explorer.candles (
  market_id       TEXT           NOT NULL,
  minutes         SMALLINT       NOT NULL,
  start_at        TIMESTAMPTZ    NOT NULL,
  open            NUMERIC        NOT NULL,     -- price of the bucket's lowest trade id
  high            NUMERIC        NOT NULL,
  low             NUMERIC        NOT NULL,
  close           NUMERIC        NOT NULL,     -- price of the bucket's highest trade id
  base_volume     NUMERIC(78,0)  NOT NULL,
  quote_volume    NUMERIC(78,0)  NOT NULL,     -- a fill's is amount × price truncated, a swap's exact
  first_trade_id  BIGINT         NOT NULL,
  last_trade_id   BIGINT         NOT NULL,
  PRIMARY KEY (market_id, minutes, start_at)
);

-- A day of a market's trades, and its first one.
CREATE INDEX idx_order_fills_market_time ON explorer.order_fills (market_id, time);
CREATE INDEX idx_swaps_pool_time         ON explorer.swaps (pool_id, time);

-- The row 'candles' asks the post-migration step "candles" to build the
-- whole history: it replaces it with a row 'candles:<market>' per market,
-- deleted once that market is built.
INSERT INTO explorer.pending_rewrites (name) VALUES ('candles');
