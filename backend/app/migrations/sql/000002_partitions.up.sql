-- Partitions of 1,000,000 heights for the seven history tables:
-- explorer.blocks_p000024 holds heights [24000000, 25000000).
-- `bze-scan migrate` calls it for [0, head + 20,000,000] after every run and
-- the live indexer calls it again as it climbs. The advisory lock serialises
-- concurrent callers, so two of them never race on the same CREATE TABLE.
-- The Go side mirrors the size, the table list and the naming in
-- app/migrations/partitions.go.
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
