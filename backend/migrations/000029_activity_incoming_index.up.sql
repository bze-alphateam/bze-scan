-- Account activity filtered to incoming transfers:
-- GET /accounts/{address}/activity?incoming=true. The predicate is the part of
-- that filter that needs no other table, word for word as the query spells it,
-- so a walk of one address's rows only meets the candidates, newest first.
CREATE INDEX idx_account_activity_incoming
  ON explorer.account_activity (address, height DESC, tx_index DESC)
  WHERE success
    AND deltas @? '$[*] ? (!(@.amount starts with "-"))'
    AND msg_types && ARRAY['/cosmos.bank.v1beta1.MsgSend', '/cosmos.bank.v1beta1.MsgMultiSend', '/cosmos.authz.v1beta1.MsgExec', '/ibc.core.channel.v1.MsgRecvPacket']::text[];
