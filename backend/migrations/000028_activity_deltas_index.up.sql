-- Account activity filtered by denom (BFE-324): GET /accounts/{address}/activity?denom=
-- keeps the rows whose deltas hold the denom (deltas @> '[{"denom":"…"}]').
-- jsonb_path_ops indexes exactly that containment and stays smaller than the
-- default GIN operator class; with the (address, height, tx_index) primary
-- key the planner combines both, or walks the key backwards for a denom the
-- address moves often.
CREATE INDEX idx_account_activity_deltas ON explorer.account_activity USING gin (deltas jsonb_path_ops);
