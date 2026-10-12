-- client_status: the light client of the channel, as the node's ClientStatus
-- query names it, lower case: active | expired | frozen (NULL = not asked yet
-- or the query failed). An expired or frozen client no longer relays, so the
-- channel is unusable until governance recovers the client.
ALTER TABLE explorer.ibc_channels ADD COLUMN client_status TEXT;
-- status: the chain registry's chain.json status: live | upcoming | killed
-- (a chain whose network was shut down).
ALTER TABLE explorer.chains ADD COLUMN status TEXT;
