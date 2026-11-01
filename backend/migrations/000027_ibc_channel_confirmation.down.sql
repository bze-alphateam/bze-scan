-- Reverts 000027_ibc_channel_confirmation.
ALTER TABLE explorer.ibc_channels DROP COLUMN IF EXISTS confirm_note;
ALTER TABLE explorer.ibc_channels DROP COLUMN IF EXISTS confirm_checked_at;
ALTER TABLE explorer.ibc_channels DROP COLUMN IF EXISTS confirmed_at;
ALTER TABLE explorer.ibc_channels DROP COLUMN IF EXISTS confirmed_via;
