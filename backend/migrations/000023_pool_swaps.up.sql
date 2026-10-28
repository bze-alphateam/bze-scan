-- Liquidity-pool swaps and the stable trade id (BFE-301).
--
-- A trade is an order book fill (explorer.order_fills) or a pool swap
-- (explorer.swaps, one row per bze.tradebin.SwapEvent). Its trade_id is
-- height × 1,000,000 + n, n being the trade's 1-based ordinal among all the
-- trades of its block: first the swaps of the transactions by tx_index,
-- msg_index and position among the message's swap events, then the
-- block-level trades (fills and the chain's own swaps) by block_events seq.
-- Chain data never changes once indexed, so neither does the id; the writer
-- fails a block with a million trades or more. The transformer
-- (internal/transform/swaps.go) is the live side of the same rule.

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

-- index_trades builds the swaps rows and the order_fills trade ids of the
-- heights [p_lo, p_hi) from what is stored: the SwapEvents of
-- messages.events and block_events, and the order_fills rows. Rows already
-- there are kept, so it can run again. It then records p_hi as the
-- 'swaps' rewrite's resume point, in the same transaction.
CREATE FUNCTION explorer.index_trades(p_lo BIGINT, p_hi BIGINT) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE worst BIGINT;
BEGIN
  DROP TABLE IF EXISTS pg_temp.trades;
  CREATE TEMP TABLE trades ON COMMIT DROP AS
  SELECT t.height, t.tx_index, t.msg_index, t.seq, t.a,
         row_number() OVER (PARTITION BY t.height ORDER BY t.grp, t.tx_index, t.msg_index, t.k, t.seq) AS n
    FROM (SELECT m.height, 0 AS grp, m.tx_index, m.msg_index, e.k, NULL::integer AS seq, e.ev->'attrs' AS a
            FROM explorer.messages m
            CROSS JOIN LATERAL jsonb_array_elements(m.events) WITH ORDINALITY AS e(ev, k)
           WHERE m.height >= p_lo AND m.height < p_hi
             AND m.events @? '$[*] ? (@.type == "bze.tradebin.SwapEvent")'
             AND e.ev->>'type' = 'bze.tradebin.SwapEvent'
          UNION ALL
          SELECT b.height, 1, NULL, NULL, NULL, b.seq, b.attrs
            FROM explorer.block_events b
           WHERE b.height >= p_lo AND b.height < p_hi AND b.type = 'bze.tradebin.SwapEvent'
          UNION ALL
          SELECT f.height, 1, NULL, NULL, NULL, f.seq, NULL
            FROM explorer.order_fills f
           WHERE f.height >= p_lo AND f.height < p_hi) t;

  SELECT max(n) INTO worst FROM trades;
  IF worst >= 1000000 THEN
    RAISE EXCEPTION 'a block in [%, %) has % trades: trade ids allow 999,999', p_lo, p_hi, worst;
  END IF;

  INSERT INTO explorer.swaps (height, trade_id, time, pool_id, base, quote, side, base_amount, quote_amount,
                              creator, tx_hash, tx_index, msg_index, block_seq)
  SELECT t.height, t.height * 1000000 + t.n, b.time, t.a->>'pool_id', d.base, d.quote,
         CASE WHEN t.a->'in'->>'denom' = d.base THEN 'sell' ELSE 'buy' END,
         (CASE WHEN t.a->'in'->>'denom' = d.base THEN t.a->'in'->>'amount' ELSE t.a->'out'->>'amount' END)::numeric,
         (CASE WHEN t.a->'in'->>'denom' = d.base THEN t.a->'out'->>'amount' ELSE t.a->'in'->>'amount' END)::numeric,
         t.a->>'creator', x.hash, t.tx_index, t.msg_index, t.seq
    FROM trades t
    JOIN explorer.blocks b ON b.height = t.height
    LEFT JOIN explorer.transactions x ON x.height = t.height AND x.tx_index = t.tx_index
    CROSS JOIN LATERAL (SELECT least(t.a->'in'->>'denom' COLLATE "C", t.a->'out'->>'denom' COLLATE "C") AS base,
                               greatest(t.a->'in'->>'denom' COLLATE "C", t.a->'out'->>'denom' COLLATE "C") AS quote) d
   WHERE t.a IS NOT NULL
  ON CONFLICT (height, trade_id) DO NOTHING;

  UPDATE explorer.order_fills f SET trade_id = t.height * 1000000 + t.n
    FROM trades t
   WHERE t.a IS NULL AND f.height = t.height AND f.seq = t.seq
     AND f.height >= p_lo AND f.height < p_hi
     AND f.trade_id IS DISTINCT FROM t.height * 1000000 + t.n;

  UPDATE explorer.pending_rewrites SET resume_from = p_hi WHERE name = 'swaps' AND resume_from < p_hi;
END $$;

-- The 'swaps' rewrite (migrations/steps.go) runs index_trades over every
-- partition from resume_from up, each in its own transaction, then deletes
-- the row.
ALTER TABLE explorer.pending_rewrites ADD COLUMN resume_from BIGINT NOT NULL DEFAULT 0;
INSERT INTO explorer.pending_rewrites (name) VALUES ('swaps');
