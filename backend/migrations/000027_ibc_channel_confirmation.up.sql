-- Whether an IBC channel's counterparty is the chain its light client
-- claims. confirmed_via: registry (the chain registry's _IBC file lists the
-- channel) or endpoint (the counterparty's public REST endpoints report the
-- same channel, connection and a client tracking BZE); NULL while
-- unconfirmed. confirm_checked_at is the last check (an unconfirmed channel
-- is checked again at most hourly), confirm_note the last failure's reason.
-- A confirmed channel is never checked again; a client, connection or
-- counterparty change resets it. No backfill: the next channels run checks.
ALTER TABLE explorer.ibc_channels ADD COLUMN confirmed_via TEXT
  CHECK (confirmed_via IN ('registry', 'endpoint'));
ALTER TABLE explorer.ibc_channels ADD COLUMN confirmed_at TIMESTAMPTZ;
ALTER TABLE explorer.ibc_channels ADD COLUMN confirm_checked_at TIMESTAMPTZ;
ALTER TABLE explorer.ibc_channels ADD COLUMN confirm_note TEXT;
