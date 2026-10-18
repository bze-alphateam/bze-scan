-- The highest block each validator signed, kept by the live writer from the
-- commit signatures of every block it writes (and of the canonical commit
-- that corrects them), so the API can tell an active validator that stopped
-- signing (online / offline). History is not filled: indexer_state
-- signing_watch_from is the live cursor now, the live writer records signers
-- from the next height on (a database without a cursor watches from its
-- live_floor).
ALTER TABLE explorer.validators
  ADD COLUMN last_signed_height BIGINT;   -- NULL: not seen signing since the watch began

INSERT INTO explorer.indexer_state (key, value, updated_at)
SELECT 'signing_watch_from', value, now() FROM explorer.indexer_state WHERE key = 'last_indexed_height'
ON CONFLICT (key) DO NOTHING;
