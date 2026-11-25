-- DAOs (chain v8.3.0, x/dao; BFE-353).
--
-- The chain keeps every DAO forever, so a DAO's current state (rules,
-- members, electorate) is read live; the explorer indexes only what the
-- chain forgets: who created a DAO, when, and every change made to it.

-- 18. DAOs
CREATE TABLE explorer.daos (
  id              BIGINT       PRIMARY KEY,
  address         TEXT         NOT NULL UNIQUE,   -- the DAO's derived treasury account
  type            TEXT         NOT NULL,          -- static_list | stake
  name            TEXT         NOT NULL DEFAULT '',
  admin           TEXT         NOT NULL DEFAULT '',   -- current admin as on chain; the DAO's own address when it owns itself
  self_governed   BOOLEAN      NOT NULL DEFAULT false, -- admin = address
  created_height  BIGINT,                         -- NULL for a DAO the index missed (created below the live floor)
  created_time    TIMESTAMPTZ,
  created_tx_hash TEXT,
  creator         TEXT,                           -- the MsgCreateDao signer
  updated_height  BIGINT,                         -- height of the latest indexed change of the DAO
  updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_daos_admin ON explorer.daos (admin, id DESC) WHERE NOT self_governed;
CREATE INDEX idx_daos_type  ON explorer.daos (type, id DESC);

-- 19. DAO history: one row per DAO event (the DAO page's History tab).
CREATE TABLE explorer.dao_events (
  height    BIGINT   NOT NULL,
  tx_index  INTEGER  NOT NULL,         -- -1 for a block-level event (a gov proposal ran a DAO message)
  msg_index INTEGER  NOT NULL,         -- -1 at block level
  seq       INTEGER  NOT NULL,         -- position among the message's events, or among the block's events
  dao_id    BIGINT   NOT NULL,
  type      TEXT     NOT NULL,         -- the event type without the bze.dao. prefix (EventDaoCreated, EventAdminChanged…)
  attrs     JSONB    NOT NULL,
  tx_hash   TEXT,                      -- NULL at block level
  time      TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (height, tx_index, msg_index, seq)
) PARTITION BY RANGE (height);

CREATE INDEX idx_dao_events_dao ON explorer.dao_events (dao_id, height DESC, tx_index DESC, msg_index DESC, seq DESC);

-- ensure_partitions creates the dao_events partitions too.
CREATE OR REPLACE FUNCTION explorer.ensure_partitions(p_from BIGINT, p_to BIGINT) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE t TEXT; lo BIGINT;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('explorer.ensure_partitions'));
  FOR lo IN SELECT generate_series((p_from / 1000000) * 1000000, p_to, 1000000) LOOP
    FOREACH t IN ARRAY ARRAY['blocks','transactions','messages','transfers','account_activity','block_events','order_fills','swaps','dao_events'] LOOP
      EXECUTE format(
        'CREATE TABLE IF NOT EXISTS explorer.%I PARTITION OF explorer.%I FOR VALUES FROM (%s) TO (%s)',
        format('%s_p%s', t, lpad((lo / 1000000)::text, 6, '0')), t, lo, lo + 1000000);
    END LOOP;
  END LOOP;
END $$;
