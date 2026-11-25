-- DAO proposals, votes and ballot reductions (chain v8.3.0, x/dao; BFE-354).
--
-- The chain deletes a finished proposal and its ballots (MsgCleanupProposal),
-- so the explorer keeps their history from the events; an open proposal's
-- running tally is read live by the dao_proposals state-sync set.

-- 20. DAO proposals. Lifecycle events can be indexed before the submission
-- (downward backfill, or a submission below the live floor), so every column
-- but the id and the status is nullable: a settle writes a row the
-- submission fills later.
CREATE TABLE explorer.dao_proposals (
  id                  BIGINT       PRIMARY KEY,         -- global proposal id (not per DAO)
  dao_id              BIGINT,
  proposer            TEXT,
  title               TEXT,
  summary             TEXT,
  msgs                JSONB,                            -- the MsgSubmitProposal body's msgs (proto JSON, with @type)
  expedited           BOOLEAN,
  submit_height       BIGINT,
  submit_time         TIMESTAMPTZ,
  submit_tx_hash      TEXT,
  voting_end          TIMESTAMPTZ,
  denominator         NUMERIC,                          -- pinned total voting power at submission
  pinned_config       JSONB,                            -- the DaoConfig pinned at submission (proto JSON), from the state sync
  status              TEXT         NOT NULL DEFAULT 'voting', -- voting | rejected | executed | execution_failed | expired | cancelled
  status_height       BIGINT,                           -- height of the event that set the status (NULL: set by the state sync)
  yes                 NUMERIC,                          -- running while voting (state sync), final at settle
  no                  NUMERIC,
  abstain             NUMERIC,
  tally_updated_at    TIMESTAMPTZ,
  expedite_settleable BOOLEAN      NOT NULL DEFAULT false, -- the node's answer at the last refresh, while voting
  retry_deadline      TIMESTAMPTZ,                      -- execution_failed: retries allowed until then
  settled_height      BIGINT,                           -- the latest MsgSettleProposal (first settle, retry or expiry)
  settled_tx_hash     TEXT,
  expedited_early     BOOLEAN,
  executed            BOOLEAN,                          -- NULL until a passed proposal ran; the latest run's outcome
  execution_error     TEXT,
  deposit_refunded    BOOLEAN,                          -- NULL until the deposit was released (none for a proposal without one)
  deposit_destination TEXT,
  deposit_amount      TEXT,                             -- the released coin, e.g. 1000ubze
  cancelled_height    BIGINT,
  cleaned_height      BIGINT,                           -- the cleanup that removed the proposal from the chain
  updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_dao_proposals_dao    ON explorer.dao_proposals (dao_id, id DESC);
CREATE INDEX idx_dao_proposals_status ON explorer.dao_proposals (dao_id, status, id DESC);
CREATE INDEX idx_dao_proposals_voting ON explorer.dao_proposals (id) WHERE status = 'voting';

-- 21. DAO votes: each voter's latest ballot (a revote replaces it).
CREATE TABLE explorer.dao_votes (
  proposal_id BIGINT      NOT NULL,
  voter       TEXT        NOT NULL,
  option      TEXT        NOT NULL,                     -- yes | no | abstain
  weight      NUMERIC     NOT NULL,                     -- EventVote.weight; never lowered by a reduction
  height      BIGINT      NOT NULL,
  tx_index    INTEGER     NOT NULL,
  msg_index   INTEGER     NOT NULL,
  tx_hash     TEXT,
  time        TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (proposal_id, voter)
);

CREATE INDEX idx_dao_votes_voter ON explorer.dao_votes (voter, height DESC);

-- 22. DAO ballot reductions: one row per EventBallotReduced, insert-only. The
-- effective weight of a vote is its weight minus the reductions after it.
CREATE TABLE explorer.dao_ballot_reductions (
  height      BIGINT   NOT NULL,
  tx_index    INTEGER  NOT NULL,
  msg_index   INTEGER  NOT NULL,
  seq         INTEGER  NOT NULL,          -- position among the message's reductions
  proposal_id BIGINT   NOT NULL,
  voter       TEXT     NOT NULL,
  source      TEXT     NOT NULL,          -- the stake source (sr:<id>, dr:<denom>) or member
  amount      NUMERIC  NOT NULL,
  tx_hash     TEXT,
  PRIMARY KEY (height, tx_index, msg_index, seq)
) PARTITION BY RANGE (height);

CREATE INDEX idx_dao_ballot_reductions_ballot ON explorer.dao_ballot_reductions (proposal_id, voter, height, tx_index);

-- ensure_partitions creates the dao_ballot_reductions partitions too.
CREATE OR REPLACE FUNCTION explorer.ensure_partitions(p_from BIGINT, p_to BIGINT) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE t TEXT; lo BIGINT;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('explorer.ensure_partitions'));
  FOR lo IN SELECT generate_series((p_from / 1000000) * 1000000, p_to, 1000000) LOOP
    FOREACH t IN ARRAY ARRAY['blocks','transactions','messages','transfers','account_activity','block_events','order_fills','swaps','dao_events','dao_ballot_reductions'] LOOP
      EXECUTE format(
        'CREATE TABLE IF NOT EXISTS explorer.%I PARTITION OF explorer.%I FOR VALUES FROM (%s) TO (%s)',
        format('%s_p%s', t, lpad((lo / 1000000)::text, 6, '0')), t, lo, lo + 1000000);
    END LOOP;
  END LOOP;
END $$;
