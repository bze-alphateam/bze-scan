-- 16. Partitions
-- Partitions of 1,000,000 heights for the seven history tables:
-- explorer.blocks_p000024 holds heights [24000000, 25000000).
-- `bze-scan migrate up` calls it after the migrations and the live indexer
-- calls it again as it climbs. The advisory lock serialises concurrent
-- callers, so two of them never race on the same CREATE TABLE. The Go side
-- mirrors the size, the table list and the naming in migrations/partitions.go.
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

-- Reclassification after the Go classification grew (section 5): activity
-- rows stored as 'other' whose first message type now has a message_kinds
-- row get its kind, and the signer or participant category depending on
-- is_signer. The kind = 'other' filter matches the partial index
-- idx_account_activity_unknown. Returns the number of rows updated.
CREATE OR REPLACE FUNCTION explorer.reclassify_unknown() RETURNS bigint
LANGUAGE plpgsql AS $$
DECLARE n BIGINT;
BEGIN
  UPDATE explorer.account_activity a
     SET kind     = k.kind,
         category = CASE WHEN a.is_signer THEN k.signer_category ELSE k.participant_category END
    FROM explorer.message_kinds k
   WHERE a.kind = 'other'
     AND k.type_url = a.msg_types[1]
     AND k.kind <> 'other';
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END $$;
