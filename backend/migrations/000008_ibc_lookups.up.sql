-- Lookups of the cross-chain transfer endpoints (BFE-99).
--
-- The transfers an acknowledgement or a timeout settled, for the
-- transaction page (idx_ibc_transfers_created_tx covers the creating side).
CREATE INDEX idx_ibc_transfers_result_tx ON explorer.ibc_transfers (result_tx_hash) WHERE result_tx_hash IS NOT NULL;
