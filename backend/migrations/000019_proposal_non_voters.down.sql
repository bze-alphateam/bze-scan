ALTER TABLE explorer.proposals
  DROP COLUMN IF EXISTS non_voters_at,
  DROP COLUMN IF EXISTS active_set_size,
  DROP COLUMN IF EXISTS active_set_height;
DROP TABLE IF EXISTS explorer.proposal_non_voters;
