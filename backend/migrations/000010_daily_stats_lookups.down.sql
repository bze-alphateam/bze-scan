-- Reverts 000010_daily_stats_lookups.
DROP INDEX IF EXISTS explorer.idx_accounts_first_seen;
DROP INDEX IF EXISTS explorer.idx_account_activity_height;
