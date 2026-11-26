-- Guardian actions run inside another message (BFE-421): the emergency
-- messages an authz MsgExec or a dao MsgAdminExecute ran in a successful
-- transaction, and those an executed DAO proposal ran at its settle. Their
-- bodies are not transaction messages, so explorer.messages does not list
-- them; the transformer unwraps the wrappers (as for token_events and
-- validator_events) and writes one row per inner emergency message. Few
-- rows, so not partitioned.

-- 25. Nested emergency actions.
CREATE TABLE explorer.emergency_nested_actions (
  height      BIGINT      NOT NULL,          -- the wrapper's transaction (a proposal's: its settle)
  tx_index    INTEGER     NOT NULL,
  msg_index   INTEGER     NOT NULL,          -- the wrapper message (a proposal's: the settle message, 0 when not stored)
  seq         INTEGER     NOT NULL,          -- the inner message's position: k for a MsgExec / MsgAdminExecute, 1000 * (rank of the proposal in its settle + 1) + k for a proposal
  type_url    TEXT        NOT NULL,          -- the inner message's type URL
  signer      TEXT,                          -- the inner message's signer or authority (the guardian)
  body        JSONB       NOT NULL,          -- the inner message (proto JSON, with @type)
  via_kind    TEXT        NOT NULL,          -- authz | dao
  grantee     TEXT,                          -- authz: the MsgExec's signer
  dao_id      BIGINT,                        -- dao: the DAO
  proposal_id BIGINT,                        -- dao: the executed proposal; NULL for a MsgAdminExecute
  tx_hash     TEXT        NOT NULL,
  time        TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (height, tx_index, msg_index, seq)
);
