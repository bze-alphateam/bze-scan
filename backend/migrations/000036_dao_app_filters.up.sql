-- DAO app filters (BFE-368): the current members, electorate sources and
-- pending admin nomination of every DAO, so lists can be filtered by them (the
-- chain has no such index). Current state only, replaced by the daos state
-- sync set on every resync of a DAO; the history is in dao_events.

-- 23. DAO members of a static-list DAO.
CREATE TABLE explorer.dao_members (
  dao_id  BIGINT NOT NULL,
  address TEXT   NOT NULL,           -- lowercase bech32
  PRIMARY KEY (dao_id, address)
);

CREATE INDEX idx_dao_members_address ON explorer.dao_members (address);

-- 24. Electorate sources of a stake DAO, in the chain's join-record form:
-- sr/{reward_id} or dr/{staking_denom}.
CREATE TABLE explorer.dao_sources (
  dao_id BIGINT NOT NULL,
  source TEXT   NOT NULL,
  PRIMARY KEY (dao_id, source)
);

CREATE INDEX idx_dao_sources_source ON explorer.dao_sources (source);

-- The address nominated as the DAO's next admin; NULL when none is pending.
ALTER TABLE explorer.daos ADD COLUMN pending_admin TEXT;
CREATE INDEX idx_daos_pending_admin ON explorer.daos (pending_admin, id DESC) WHERE pending_admin IS NOT NULL;
