-- Reverts 000008_ibc_lookups.
DROP INDEX IF EXISTS explorer.idx_ibc_transfers_result_tx;
