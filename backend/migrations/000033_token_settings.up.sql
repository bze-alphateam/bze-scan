-- BFE-361: factory token settings of chain v8.3.0. mintable is NULL until the
-- denoms set read it from a v8.3.0 node (a v8.2.x authority has no such
-- field); community_pool is NULL for "no pool"; the ratios are NULL while the
-- config is unknown (not a factory denom, or a v8.2.x node).
ALTER TABLE explorer.denoms
  ADD COLUMN mintable       boolean,
  ADD COLUMN community_pool text,
  ADD COLUMN transfer_fee   numeric,
  ADD COLUMN gas_surcharge  numeric;
