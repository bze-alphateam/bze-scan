-- Emergency chain halts (chain v8.3.0, x/guardian MsgHaltChain): the
-- guardian or governance schedules an x/upgrade plan "emergency-halt-<H>"
-- at the next block H, replacing any plan already scheduled. No proposal is
-- behind it, so proposal_id becomes optional and the row says which kind it
-- is. The halt itself is the same as a governance upgrade's: consensus
-- waits at H + 1.
ALTER TABLE explorer.upgrades
  ALTER COLUMN proposal_id DROP NOT NULL,
  ADD COLUMN kind TEXT NOT NULL DEFAULT 'governance' CHECK (kind IN ('governance', 'emergency')),
  ADD COLUMN reason TEXT,                  -- emergency: the reason the signer gave
  ADD COLUMN scheduled_by TEXT,            -- emergency: the signer (the guardian, or the gov module)
  ADD COLUMN scheduled_tx_hash TEXT,       -- emergency: the transaction; NULL when governance ran it at block level
  ADD COLUMN superseded_by_height BIGINT;  -- governance: an emergency halt at this height replaced the plan on chain
