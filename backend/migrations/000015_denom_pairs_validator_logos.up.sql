-- lp_base/lp_quote: the two pool assets of an LP denom, from the pool's stored
-- lp_denom (ulp/<hash> is not reversible) or, for a legacy ulp_<base>_<quote>
-- missing from the pool list, from the denom itself.
ALTER TABLE explorer.denoms ADD COLUMN lp_base TEXT;
ALTER TABLE explorer.denoms ADD COLUMN lp_quote TEXT;
-- via_chain_id: for a multi-hop IBC denom, the chain of the first hop (the
-- counterparty of the BZE channel); NULL for one hop.
ALTER TABLE explorer.denoms ADD COLUMN via_chain_id TEXT;
-- logo_url: the Keybase picture of the validator's identity; logo_checked_at:
-- the last successful lookup (NULL = never checked).
ALTER TABLE explorer.validators ADD COLUMN logo_url TEXT;
ALTER TABLE explorer.validators ADD COLUMN logo_checked_at TIMESTAMPTZ;
