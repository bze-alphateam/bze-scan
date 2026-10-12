-- Reverts 000011_snapshot_sources.
ALTER TABLE explorer.proposal_deposits DROP COLUMN IF EXISTS source;
ALTER TABLE explorer.proposal_votes DROP COLUMN IF EXISTS source;
ALTER TABLE explorer.proposals DROP COLUMN IF EXISTS tally_source;
