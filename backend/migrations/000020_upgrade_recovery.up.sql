-- When each validator came back after a software-upgrade halt (BFE-194).
--
-- At an upgrade's plan height U the old binary decides block U and stops
-- while executing it, so consensus waits at U + 1 until two thirds of the
-- power runs the new binary. The halt watch records the validators it sees
-- voting at U + 1 during the halt (live only); the upgrade_recovery
-- state-sync set fills the others from the commits of U + 1 onwards.
CREATE TABLE explorer.upgrades (
  height             BIGINT       PRIMARY KEY,   -- the plan height U
  proposal_id        BIGINT       NOT NULL,      -- the passed software_upgrade proposal
  name               TEXT         NOT NULL,      -- the plan name
  halt_time          TIMESTAMPTZ,                -- block U was decided: the time of block U + 1
  resume_time        TIMESTAMPTZ,                -- block U + 1 was decided: the time of block U + 2
  active_set_size    INTEGER,                    -- validators of the set at U + 1
  watched            BOOLEAN      NOT NULL DEFAULT false,  -- the halt watch saw the halt's votes
  window_end_height  BIGINT,                     -- the last height whose commit was read
  computed_at        TIMESTAMPTZ                 -- NULL: pending
);
CREATE INDEX upgrades_proposal_idx ON explorer.upgrades (proposal_id);

-- One row per member of the validator set at U + 1.
CREATE TABLE explorer.upgrade_recovery (
  upgrade_height       BIGINT       NOT NULL,
  consensus_address    TEXT         NOT NULL,   -- upper-case hex
  operator_address     TEXT,                    -- NULL: no validator known with this consensus address
  voting_power         BIGINT       NOT NULL,   -- consensus power in the set at U + 1
  back_time            TIMESTAMPTZ,             -- when it came back; NULL: not back in the window
  back_source          TEXT CHECK (back_source IN ('halt_vote', 'commit')),
  vote_time            TIMESTAMPTZ,             -- its own timestamp on that vote or signature
  first_signed_height  BIGINT,                  -- source commit: the first height it signed
  blocks_needed        BIGINT,                  -- source commit: heights after U + 1 before it signed
  seconds_back         INTEGER,                 -- back_time - halt_time
  PRIMARY KEY (upgrade_height, consensus_address)
);
