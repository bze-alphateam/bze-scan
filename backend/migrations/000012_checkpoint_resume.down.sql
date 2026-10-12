-- Reverts 000012_checkpoint_resume.
ALTER TABLE explorer.backfill_checkpoints DROP COLUMN IF EXISTS resume_height;
