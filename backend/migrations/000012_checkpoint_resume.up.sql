-- The exact resume point of a backfill job: every height above
-- resume_height was written or recorded in index_failures. lowest_dispatched
-- alone is not enough: a height held up by retries, or in flight when the
-- process stopped, can lie any number of heights above it.
--
-- Existing rows resume at their ceiling; the backfill skips the heights
-- already indexed, so that rescan is cheap and refills any hole left by the
-- old resume margin.
ALTER TABLE explorer.backfill_checkpoints ADD COLUMN resume_height BIGINT;
UPDATE explorer.backfill_checkpoints SET resume_height = ceiling_height;
ALTER TABLE explorer.backfill_checkpoints ALTER COLUMN resume_height SET NOT NULL;
