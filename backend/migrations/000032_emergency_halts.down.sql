-- Reverts 000032_emergency_halts: the emergency halts and their recovery
-- are forgotten.
DELETE FROM explorer.upgrade_recovery r
 USING explorer.upgrades u
 WHERE u.height = r.upgrade_height AND u.kind = 'emergency';
DELETE FROM explorer.upgrades WHERE kind = 'emergency';
ALTER TABLE explorer.upgrades
  DROP COLUMN IF EXISTS superseded_by_height,
  DROP COLUMN IF EXISTS scheduled_tx_hash,
  DROP COLUMN IF EXISTS scheduled_by,
  DROP COLUMN IF EXISTS reason,
  DROP COLUMN IF EXISTS kind,
  ALTER COLUMN proposal_id SET NOT NULL;
