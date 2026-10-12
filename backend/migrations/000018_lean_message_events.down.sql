-- Reverts 000018_lean_message_events. The events it removed do not come back:
-- `bze-scan reindex --heights …` restores them.
DROP TABLE IF EXISTS explorer.pending_rewrites;
DROP FUNCTION IF EXISTS explorer.kept_events(JSONB);
