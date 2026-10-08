-- Reverts 000005_operational.

DROP TABLE IF EXISTS explorer.sync_jobs;
DROP TABLE IF EXISTS explorer.backfill_checkpoints;
DROP TABLE IF EXISTS explorer.index_failures;
DROP TABLE IF EXISTS explorer.indexer_state;
