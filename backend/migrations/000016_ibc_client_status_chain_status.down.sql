-- Reverts 000016_ibc_client_status_chain_status.
ALTER TABLE explorer.chains DROP COLUMN IF EXISTS status;
ALTER TABLE explorer.ibc_channels DROP COLUMN IF EXISTS client_status;
