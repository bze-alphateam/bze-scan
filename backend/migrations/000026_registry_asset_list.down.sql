-- Reverts 000026_registry_asset_list.
ALTER TABLE explorer.registry_assets DROP COLUMN IF EXISTS description;
ALTER TABLE explorer.registry_assets DROP COLUMN IF EXISTS logos;
ALTER TABLE explorer.chains DROP COLUMN IF EXISTS rpc_urls;
ALTER TABLE explorer.chains DROP COLUMN IF EXISTS rest_urls;
ALTER TABLE explorer.chains DROP COLUMN IF EXISTS logos;
