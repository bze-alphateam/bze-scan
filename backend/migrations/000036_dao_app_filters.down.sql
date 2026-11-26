-- Reverts 000036_dao_app_filters.
DROP INDEX IF EXISTS explorer.idx_daos_pending_admin;
ALTER TABLE explorer.daos DROP COLUMN IF EXISTS pending_admin;
DROP TABLE IF EXISTS explorer.dao_sources;
DROP TABLE IF EXISTS explorer.dao_members;
