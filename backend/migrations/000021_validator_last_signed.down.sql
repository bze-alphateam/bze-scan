DELETE FROM explorer.indexer_state WHERE key = 'signing_watch_from';
ALTER TABLE explorer.validators DROP COLUMN IF EXISTS last_signed_height;
