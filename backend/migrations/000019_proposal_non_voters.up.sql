-- The validators of the set a closed proposal's tally counted that did not
-- vote (BFE-192), written by the proposal_non_voters state-sync set: the
-- CometBFT validator set at proposals.active_set_height, minus the
-- validators jailed in the tally's block, minus those whose account voted.
-- Only the non-voters are stored, never the whole set.
CREATE TABLE explorer.proposal_non_voters (
  proposal_id        BIGINT        NOT NULL,
  consensus_address  TEXT          NOT NULL,   -- upper-case hex
  operator_address   TEXT,                     -- NULL: no validator known with this consensus address
  voting_power       BIGINT        NOT NULL,   -- consensus power (bonded tokens / 10^6) at the tally
  power_pct          NUMERIC(8,5)  NOT NULL,   -- share of the tally set's power, percent
  PRIMARY KEY (proposal_id, consensus_address)
);

ALTER TABLE explorer.proposals
  ADD COLUMN active_set_height BIGINT,         -- the /validators height the tally set was read at
  ADD COLUMN active_set_size   INTEGER,        -- validators in the tally set
  ADD COLUMN non_voters_at     TIMESTAMPTZ;    -- when the non-voters were written; NULL: not computed yet
