-- Reverts 000023_pool_swaps.
DELETE FROM explorer.pending_rewrites WHERE name = 'swaps';
ALTER TABLE explorer.pending_rewrites DROP COLUMN IF EXISTS resume_from;
DROP FUNCTION IF EXISTS explorer.index_trades(BIGINT, BIGINT);

CREATE OR REPLACE FUNCTION explorer.ensure_partitions(p_from BIGINT, p_to BIGINT) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE t TEXT; lo BIGINT;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('explorer.ensure_partitions'));
  FOR lo IN SELECT generate_series((p_from / 1000000) * 1000000, p_to, 1000000) LOOP
    FOREACH t IN ARRAY ARRAY['blocks','transactions','messages','transfers','account_activity','block_events','order_fills'] LOOP
      EXECUTE format(
        'CREATE TABLE IF NOT EXISTS explorer.%I PARTITION OF explorer.%I FOR VALUES FROM (%s) TO (%s)',
        format('%s_p%s', t, lpad((lo / 1000000)::text, 6, '0')), t, lo, lo + 1000000);
    END LOOP;
  END LOOP;
END $$;

ALTER TABLE explorer.order_fills DROP COLUMN IF EXISTS trade_id;
DROP TABLE IF EXISTS explorer.swaps;
