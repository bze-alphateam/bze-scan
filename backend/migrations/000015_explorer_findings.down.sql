-- Reverts 000015_explorer_findings.
ALTER TABLE explorer.validators DROP COLUMN IF EXISTS logo_checked_at;
ALTER TABLE explorer.validators DROP COLUMN IF EXISTS logo_url;
ALTER TABLE explorer.denoms DROP COLUMN IF EXISTS via_chain_id;
ALTER TABLE explorer.denoms DROP COLUMN IF EXISTS lp_quote;
ALTER TABLE explorer.denoms DROP COLUMN IF EXISTS lp_base;
