-- Reverts 000034_daos.
DELETE FROM explorer.labels WHERE kind = 'dao';

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

DROP TABLE IF EXISTS explorer.dao_events;
DROP TABLE IF EXISTS explorer.daos;
