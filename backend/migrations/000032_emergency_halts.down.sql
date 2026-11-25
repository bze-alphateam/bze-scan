-- Reverts 000032_emergency_halts: the emergency halts without a proposal
-- and their recovery are forgotten. A governance row an emergency halt took
-- over at the same height keeps its proposal and becomes governance again.
DELETE FROM explorer.upgrade_recovery r
 USING explorer.upgrades u
 WHERE u.height = r.upgrade_height AND u.proposal_id IS NULL;
DELETE FROM explorer.upgrades WHERE proposal_id IS NULL;
ALTER TABLE explorer.upgrades
  DROP COLUMN IF EXISTS superseded_by_height,
  DROP COLUMN IF EXISTS scheduled_tx_hash,
  DROP COLUMN IF EXISTS scheduled_by,
  DROP COLUMN IF EXISTS reason,
  DROP COLUMN IF EXISTS kind,
  ALTER COLUMN proposal_id SET NOT NULL;
