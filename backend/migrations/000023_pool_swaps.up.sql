-- Liquidity-pool swaps and the stable trade id (BFE-301).
--
-- A trade is an order book fill (explorer.order_fills) or a pool swap
-- (explorer.swaps, one row per bze.tradebin.SwapEvent). Its trade_id is
-- height × 1,000,000 + n, n being the trade's 1-based ordinal among all the
-- trades of its block: first the swaps of the transactions by tx_index,
-- msg_index and position among the message's swap events, then the
-- block-level trades (fills and the chain's own swaps) by block_events seq.
-- Chain data never changes once indexed, so neither does the id; the
-- transformer fails a block with a million trades or more.
-- transform.NumberTrades holds the rule, for the transformer and for
-- internal/trades, which numbers the rows already stored.

-- 17. Pool swaps
CREATE TABLE explorer.swaps (
  height        BIGINT         NOT NULL,
  trade_id      BIGINT         NOT NULL,
  time          TIMESTAMPTZ    NOT NULL,     -- block time
  pool_id       TEXT           NOT NULL,     -- <base>_<quote>
  base          TEXT           NOT NULL,     -- the pool's byte-wise smaller denom
  quote         TEXT           NOT NULL,
  side          TEXT           NOT NULL,     -- sell when the swap paid in base, buy otherwise
  base_amount   NUMERIC(78,0)  NOT NULL,     -- the swap's base-side amount (in or out)
  quote_amount  NUMERIC(78,0)  NOT NULL,
  creator       TEXT           NOT NULL,
  tx_hash       TEXT,                        -- NULL for a block-level swap
  tx_index      INTEGER,                     -- NULL for a block-level swap
  msg_index     INTEGER,                     -- NULL for a block-level swap
  block_seq     INTEGER,                     -- block_events seq of a block-level swap, NULL otherwise
  PRIMARY KEY (height, trade_id)
) PARTITION BY RANGE (height);

CREATE INDEX idx_swaps_pool    ON explorer.swaps (pool_id, height DESC);
CREATE INDEX idx_swaps_creator ON explorer.swaps (creator, height DESC);

ALTER TABLE explorer.order_fills ADD COLUMN trade_id BIGINT;   -- NULL until the swaps rewrite below has run

-- ensure_partitions creates the swaps partitions too.
CREATE OR REPLACE FUNCTION explorer.ensure_partitions(p_from BIGINT, p_to BIGINT) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE t TEXT; lo BIGINT;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('explorer.ensure_partitions'));
  FOR lo IN SELECT generate_series((p_from / 1000000) * 1000000, p_to, 1000000) LOOP
    FOREACH t IN ARRAY ARRAY['blocks','transactions','messages','transfers','account_activity','block_events','order_fills','swaps'] LOOP
      EXECUTE format(
        'CREATE TABLE IF NOT EXISTS explorer.%I PARTITION OF explorer.%I FOR VALUES FROM (%s) TO (%s)',
        format('%s_p%s', t, lpad((lo / 1000000)::text, 6, '0')), t, lo, lo + 1000000);
    END LOOP;
  END LOOP;
END $$;

-- The fills no transformer numbered (written by the previous release while
-- both run): internal/trades looks them up here, an empty index otherwise.
CREATE INDEX idx_order_fills_unnumbered ON explorer.order_fills (height) WHERE trade_id IS NULL;

-- The row 'swaps' is internal/trades' cursor: resume_from is the lowest
-- height whose stored trades it has not numbered yet. The post-migration
-- step and the serve component move it, one transaction per range; it is
-- never deleted.
ALTER TABLE explorer.pending_rewrites ADD COLUMN resume_from BIGINT NOT NULL DEFAULT 0;
INSERT INTO explorer.pending_rewrites (name) VALUES ('swaps');
