-- Reverts 000033_token_settings.
ALTER TABLE explorer.denoms
  DROP COLUMN IF EXISTS mintable,
  DROP COLUMN IF EXISTS community_pool,
  DROP COLUMN IF EXISTS transfer_fee,
  DROP COLUMN IF EXISTS gas_surcharge;
