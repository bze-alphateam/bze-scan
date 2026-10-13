-- Message events stop storing what the explorer keeps elsewhere (BFE-173):
-- the message event (the type_url, module and sender columns), transfer,
-- coin_spent and coin_received (the explorer.transfers rows built from
-- them) and update_client's header (a hex copy of the header the message
-- body carries). The transformer leaves them out of new rows
-- (transform.UnstoredEventTypes / UnstoredEventAttributes); kept_events is
-- the same filter for the rows already stored.
CREATE FUNCTION explorer.kept_events(events JSONB) RETURNS JSONB
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT coalesce(jsonb_agg(
           CASE WHEN e->>'type' = 'update_client' THEN jsonb_set(e, '{attrs}', (e->'attrs') - 'header') ELSE e END
           ORDER BY i), '[]'::jsonb)
  FROM jsonb_array_elements(events) WITH ORDINALITY AS x(e, i)
  WHERE e->>'type' NOT IN ('message', 'transfer', 'coin_spent', 'coin_received')
$$;

-- pending_rewrites marks a rewrite of stored rows that the post-migration
-- step of the same name still has to run. A migration file runs in one
-- transaction, so the step rewrites partition by partition, each in its own,
-- and deletes the row once every partition is done; an interrupted run
-- resumes at the next `migrate up`.
CREATE TABLE explorer.pending_rewrites (
  name TEXT PRIMARY KEY
);
INSERT INTO explorer.pending_rewrites (name) VALUES ('message_events');
