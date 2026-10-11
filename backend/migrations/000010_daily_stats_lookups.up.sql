-- Lookups of the daily statistics (BFE-102): the nightly aggregation and
-- the stats endpoint's live "today" read one day's addresses.
--
-- The distinct addresses active at a height range, read from the index
-- alone.
CREATE INDEX idx_account_activity_height ON explorer.account_activity (height, address);
-- The accounts first seen in a time window.
CREATE INDEX idx_accounts_first_seen ON explorer.accounts (first_seen_time);
