-- The seven history tables, partitioned by range of height (sections 1 to 7
-- of the schema design). Their partitions are created by
-- explorer.ensure_partitions (migration 6), which `bze-scan migrate up` calls
-- after the migrations.

-- 1. Blocks
CREATE TABLE explorer.blocks (
  height                BIGINT       NOT NULL,
  time                  TIMESTAMPTZ  NOT NULL,   -- header time from /block
  tx_count              INTEGER      NOT NULL DEFAULT 0,
  tx_failed_count       INTEGER      NOT NULL DEFAULT 0,
  block_time_ms         INTEGER,                 -- time minus the previous block's time, when it is indexed
  hash                  TEXT         NOT NULL,
  proposer_cons_address TEXT,                    -- hex, as /block reports it; joins validators.consensus_address
  size_bytes            INTEGER,
  minted                NUMERIC(78,0),           -- mint event: amount (ubze)
  inflation             NUMERIC(20,18),          -- mint event: inflation
  fees_distributed      JSONB,                   -- coins moved fee_collector -> distribution in BeginBlock
  signatures_count      SMALLINT,                -- /commit?height=N : signatures present
  signatures_power_pct  NUMERIC(6,3),            -- share of voting power that signed
  PRIMARY KEY (height)
) PARTITION BY RANGE (height);

CREATE INDEX idx_blocks_time     ON explorer.blocks (time DESC);
CREATE INDEX idx_blocks_proposer ON explorer.blocks (proposer_cons_address, height DESC);

-- 2. Transactions
CREATE TABLE explorer.transactions (
  height      BIGINT       NOT NULL,
  tx_index    INTEGER      NOT NULL,
  hash        TEXT         NOT NULL,            -- 64 hex chars, upper case, as in tx_results.tx_hash
  time        TIMESTAMPTZ  NOT NULL,            -- copy of blocks.time
  success     BOOLEAN      NOT NULL,            -- code = 0
  code        INTEGER      NOT NULL,            -- block_results txs_results[index].code; 0 on success
  codespace   TEXT,
  error_log   TEXT,                             -- failed transactions only
  gas_wanted  BIGINT,
  gas_used    BIGINT,
  fee         JSONB        NOT NULL DEFAULT '[]', -- coins, from tx.fee
  fee_payer   TEXT,                             -- tx.fee_payer: the granter when a fee grant was used
  signers     TEXT[]       NOT NULL DEFAULT '{}', -- from tx.acc_seq ("address/sequence"), in order
  memo        TEXT,                             -- decoded from the raw transaction bytes in /block
  msg_count   INTEGER      NOT NULL DEFAULT 0,
  msg_types   TEXT[]       NOT NULL DEFAULT '{}', -- type URLs in message order; for failed txs decoded from the raw transaction
  PRIMARY KEY (height, tx_index)
) PARTITION BY RANGE (height);

CREATE UNIQUE INDEX uq_transactions_hash ON explorer.transactions (hash, height);  -- lookups by hash use the leading column
CREATE INDEX idx_transactions_time      ON explorer.transactions (time DESC);      -- "transactions in the last 24 h"

-- 3. Messages
CREATE TABLE explorer.messages (
  height     BIGINT   NOT NULL,
  tx_index   INTEGER  NOT NULL,
  msg_index  INTEGER  NOT NULL,
  type_url   TEXT     NOT NULL,   -- message.action, e.g. /cosmos.bank.v1beta1.MsgSend, /bze.tradebin.MsgCreateOrder
  sender     TEXT,                -- message.sender: the message's first signer
  module     TEXT,                -- message.module
  events     JSONB    NOT NULL DEFAULT '[]',
      -- the message's events in emission order:
      -- [{"type":"transfer","attrs":{"sender":"bze1…","recipient":"bze1…","amount":"10000000ubze"}}, …]
      -- typed-event values decoded to real JSON types
  body       JSONB,               -- the decoded message as proto JSON, stored for EVERY message and kept (decided 2026-10-06)
  PRIMARY KEY (height, tx_index, msg_index)
) PARTITION BY RANGE (height);

CREATE INDEX idx_messages_type ON explorer.messages (type_url, height DESC);

-- 4. Transfers (the funds flow)
CREATE TABLE explorer.transfers (
  height     BIGINT        NOT NULL,
  tx_index   INTEGER       NOT NULL,   -- -1 for BeginBlock/EndBlock transfers
  seq        INTEGER       NOT NULL,   -- order of appearance; one row per coin of a multi-coin amount
  msg_index  INTEGER,                  -- NULL for the fee transfer (ante handler) and for block-level rows
  kind       TEXT          NOT NULL,   -- transfer | mint | burn
  sender     TEXT,                     -- NULL when kind = mint
  recipient  TEXT,                     -- NULL when kind = burn
  denom      TEXT          NOT NULL,
  amount     NUMERIC(78,0) NOT NULL,
  PRIMARY KEY (height, tx_index, seq)
) PARTITION BY RANGE (height);

CREATE INDEX idx_transfers_denom ON explorer.transfers (denom, height DESC);   -- token page: moved in 24 h

-- 5. Accounts, activity and the classification tables
CREATE TABLE explorer.account_activity (
  address       TEXT         NOT NULL,
  height        BIGINT       NOT NULL,
  tx_index      INTEGER      NOT NULL,            -- -1 for block-level rows (one per address per block)
  time          TIMESTAMPTZ  NOT NULL,
  category      TEXT         NOT NULL,            -- sent | received | staking | dex | governance | tokens | rewards | burner | cross_chain | other
  kind          TEXT         NOT NULL,            -- from message_kinds / block_event_kinds: send, delegate, claim_rewards, dex_order, dex_fill, vote, ibc_out, ibc_in, ibc_refund, fee_only, other, …
  is_signer     BOOLEAN      NOT NULL DEFAULT false,
  success       BOOLEAN      NOT NULL DEFAULT true,
  counterparty  TEXT,                             -- the other party when there is exactly one: account, validator operator or module account
  deltas        JSONB        NOT NULL DEFAULT '[]', -- net balance change of the address: [{"denom":"ubze","amount":"-10002000"}]
  msg_types     TEXT[]       NOT NULL DEFAULT '{}', -- tx rows: type URLs of the messages involving the address
  details       JSONB,                            -- block-level rows: the typed events naming the address (fills, payouts, raffle results)
  PRIMARY KEY (address, height, tx_index)
) PARTITION BY RANGE (height);

CREATE INDEX idx_account_activity_category
  ON explorer.account_activity (address, category, height DESC, tx_index DESC);
CREATE INDEX idx_account_activity_unknown
  ON explorer.account_activity (height) WHERE kind = 'other';   -- rows to reclassify when a mapping is added

-- 6. Block-level events
CREATE TABLE explorer.block_events (
  height  BIGINT   NOT NULL,
  seq     INTEGER  NOT NULL,          -- position in the block's finalize_block_events list
  type    TEXT     NOT NULL,
  attrs   JSONB    NOT NULL,          -- decoded attributes
  PRIMARY KEY (height, seq)
) PARTITION BY RANGE (height);

CREATE INDEX idx_block_events_type ON explorer.block_events (type, height DESC);

-- 7. DEX orders and fills
CREATE TABLE explorer.order_fills (              -- one row per OrderExecutedEvent: a queue message filling (part of) a resting order
  height            BIGINT         NOT NULL,
  seq               INTEGER        NOT NULL,     -- block_events.seq of the event
  order_id          TEXT           NOT NULL,     -- the resting (maker) order
  market_id         TEXT           NOT NULL,
  side              TEXT           NOT NULL,     -- side of the resting order: buy | sell
  price             NUMERIC(60,18) NOT NULL,
  amount            NUMERIC(78,0)  NOT NULL,     -- base-denom amount executed
  maker             TEXT           NOT NULL,     -- owner of the resting order
  taker             TEXT           NOT NULL,     -- owner of the queue message that filled it
  taker_message_id  TEXT,                        -- queue message id of the taker's message (BZE-150, v8.2.0); NULL before that release
  taker_tx_hash     TEXT,                        -- the taker's transaction (normally in the same block), when attributable
  time              TIMESTAMPTZ    NOT NULL,
  PRIMARY KEY (height, seq)
) PARTITION BY RANGE (height);

CREATE INDEX idx_order_fills_order  ON explorer.order_fills (order_id, height DESC);
CREATE INDEX idx_order_fills_market ON explorer.order_fills (market_id, height DESC);   -- trade history, candles
CREATE INDEX idx_order_fills_taker  ON explorer.order_fills (taker, height DESC);
