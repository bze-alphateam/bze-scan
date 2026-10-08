-- The explorer schema: every table and index the explorer owns. Transcribed
-- from the database schema design (sections 1 to 15); the sink tables in
-- public are not touched here. The seven history tables are partitioned by
-- height; their partitions are created by explorer.ensure_partitions
-- (migration 2), called by `bze-scan migrate` after every run.

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE SCHEMA IF NOT EXISTS explorer;

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
CREATE TABLE explorer.accounts (
  address            TEXT         NOT NULL PRIMARY KEY,
  first_seen_height  BIGINT       NOT NULL,
  first_seen_time    TIMESTAMPTZ  NOT NULL,
  last_seen_height   BIGINT       NOT NULL,
  tx_count           BIGINT       NOT NULL DEFAULT 0,   -- transactions signed by the address
  activity_count     BIGINT       NOT NULL DEFAULT 0    -- account_activity rows
);

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

CREATE TABLE explorer.message_kinds (            -- mirror of the Go classification, rewritten by `migrate`; one row per message type URL
  type_url              TEXT PRIMARY KEY,
  kind                  TEXT NOT NULL,
  signer_category       TEXT NOT NULL,
  participant_category  TEXT NOT NULL DEFAULT 'received',
  counterparty_attr     TEXT                    -- "event.attribute" naming the counterparty, e.g. transfer.recipient, delegate.validator
);

CREATE TABLE explorer.block_event_kinds (        -- mirror of the Go classification; an event type absent here is not stored in block_events
  event_type     TEXT PRIMARY KEY,               -- e.g. bze.tradebin.OrderExecutedEvent, complete_unbonding
  kind           TEXT NOT NULL,
  category       TEXT NOT NULL,
  address_attrs  TEXT[] NOT NULL DEFAULT '{}'    -- attributes that name accounts: {maker,taker}, {delegator}, {winners}
);

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
CREATE TABLE explorer.orders (                   -- one row per order that rested on the book (OrderSavedEvent); kept forever
  order_id         TEXT           PRIMARY KEY,   -- tradebin order id, 24 zero-filled digits, unique chain-wide
  market_id        TEXT           NOT NULL,
  owner            TEXT           NOT NULL,
  side             TEXT           NOT NULL,      -- buy | sell (order_type)
  price            NUMERIC(60,18) NOT NULL,      -- the event's decimal string
  amount           NUMERIC(78,0)  NOT NULL,      -- base-denom amount that rested on the book (after the fills made when the message was processed)
  filled_amount    NUMERIC(78,0)  NOT NULL DEFAULT 0,  -- Σ order_fills.amount of this order (maker side)
  status           TEXT           NOT NULL,      -- open | filled | cancelled
  created_height   BIGINT         NOT NULL,      -- block whose EndBlock saved it
  created_time     TIMESTAMPTZ    NOT NULL,
  created_tx_hash  TEXT,                         -- the MsgCreateOrder / MsgFillOrders that placed it, when attributable (see below)
  message_id       TEXT,                         -- queue message id of the message that placed it (BZE-150, v8.2.0); NULL before that release
  closed_height    BIGINT,                       -- last fill or the cancel
  closed_time      TIMESTAMPTZ,
  updated_at       TIMESTAMPTZ    NOT NULL
);

CREATE INDEX idx_orders_owner  ON explorer.orders (owner, created_height DESC);
CREATE INDEX idx_orders_market ON explorer.orders (market_id, status, created_height DESC);

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

CREATE TABLE explorer.order_messages (           -- one row per transaction-time tradebin order event (chain v8.2.0+); the key that links a tx to its outcome; kept forever
  height      BIGINT   NOT NULL,
  message_id  TEXT     NOT NULL,                 -- queue message id, 24 zero-filled digits: unique within a block, reused by later blocks once the queue has emptied
  tx_index    INTEGER  NOT NULL,
  msg_index   INTEGER  NOT NULL,                 -- a MsgFillOrders has several rows under one msg_index (one per queued fill)
  tx_hash     TEXT     NOT NULL,
  kind        TEXT     NOT NULL,                 -- create | cancel | fill, from the message type URL
  PRIMARY KEY (height, message_id)
);

CREATE INDEX idx_order_messages_id ON explorer.order_messages (message_id, height DESC);   -- "latest transaction with this id at or below height H"

-- 8. Validators
CREATE TABLE explorer.validators (
  operator_address            TEXT          PRIMARY KEY,   -- bzevaloper1…
  account_address             TEXT          NOT NULL,      -- bze1… owner (same key bytes); votes, self-stake, commission land here
  consensus_address           TEXT,                        -- hex, as /block reports the proposer
  consensus_pubkey            TEXT,
  moniker                     TEXT          NOT NULL,
  identity                    TEXT,
  website                     TEXT,
  security_contact            TEXT,
  details                     TEXT,
  status                      TEXT          NOT NULL,      -- bonded | unbonding | unbonded
  jailed                      BOOLEAN       NOT NULL DEFAULT false,
  tombstoned                  BOOLEAN       NOT NULL DEFAULT false,
  jailed_until                TIMESTAMPTZ,
  tokens                      NUMERIC(78,0) NOT NULL,
  delegator_shares            NUMERIC(96,18) NOT NULL,
  commission_rate             NUMERIC(20,18) NOT NULL,
  commission_max_rate         NUMERIC(20,18) NOT NULL,
  commission_max_change_rate  NUMERIC(20,18) NOT NULL,
  commission_update_time      TIMESTAMPTZ,
  min_self_delegation         NUMERIC(78,0),
  self_delegation             NUMERIC(78,0),               -- owner's own delegation
  delegator_count             INTEGER,
  rank                        INTEGER,                     -- by tokens among bonded validators
  voting_power_pct            NUMERIC(8,5),
  missed_blocks               INTEGER,                     -- slashing signing info, current window
  signed_blocks_window        INTEGER,
  first_seen_height           BIGINT,                      -- earliest validator_events.created, else first sync
  first_seen_time             TIMESTAMPTZ,
  updated_at                  TIMESTAMPTZ   NOT NULL
);

CREATE INDEX idx_validators_cons          ON explorer.validators (consensus_address);
CREATE INDEX idx_validators_account       ON explorer.validators (account_address);
CREATE INDEX idx_validators_moniker_trgm  ON explorer.validators USING GIN (moniker gin_trgm_ops);

CREATE TABLE explorer.validator_events (
  height            BIGINT       NOT NULL,
  tx_index          INTEGER      NOT NULL,   -- -1 for block-level (slash) and sync-detected changes
  seq               INTEGER      NOT NULL,
  operator_address  TEXT         NOT NULL,
  kind              TEXT         NOT NULL,   -- created | commission_changed | description_changed | jailed | unjailed | slashed | tombstoned | bonded | unbonded
  details           JSONB,                   -- {"from":"0.04","to":"0.05"} | {"reason":"missing_signature","burned":"…","power":"…"}
  time              TIMESTAMPTZ  NOT NULL,
  PRIMARY KEY (height, tx_index, seq)
);

CREATE INDEX idx_validator_events_val ON explorer.validator_events (operator_address, height DESC);

-- 9. Governance
CREATE TABLE explorer.proposals (
  id                   BIGINT        PRIMARY KEY,
  title                TEXT          NOT NULL,
  summary              TEXT,
  metadata             TEXT,
  proposer             TEXT,
  kind                 TEXT          NOT NULL,   -- software_upgrade | community_pool_spend | parameter_change | cointrunk_publisher | ibc_client_update | text | other
  message_types        TEXT[]        NOT NULL DEFAULT '{}',
  messages             JSONB,                    -- proposal messages as proto JSON; drives the plain-word renderers (plan height, recipient/amount, before/after)
  status               TEXT          NOT NULL,   -- deposit_period | voting_period | passed | rejected | failed
  expedited            BOOLEAN       NOT NULL DEFAULT false,
  submit_time          TIMESTAMPTZ   NOT NULL,
  deposit_end_time     TIMESTAMPTZ,
  voting_start_time    TIMESTAMPTZ,
  voting_end_time      TIMESTAMPTZ,
  total_deposit        JSONB,
  tally_yes            NUMERIC(78,0),
  tally_no             NUMERIC(78,0),
  tally_abstain        NUMERIC(78,0),
  tally_veto           NUMERIC(78,0),
  tally_bonded_tokens  NUMERIC(78,0),            -- bonded supply at tally time, for turnout
  tally_updated_at     TIMESTAMPTZ,
  submit_height        BIGINT,
  submit_tx_hash       TEXT,
  resolved_height      BIGINT,                   -- block in which active_proposal fired; where a passed proposal executed
  updated_at           TIMESTAMPTZ   NOT NULL
);

CREATE INDEX idx_proposals_status      ON explorer.proposals (status, id DESC);
CREATE INDEX idx_proposals_title_trgm  ON explorer.proposals USING GIN (title gin_trgm_ops);

CREATE TABLE explorer.proposal_votes (
  proposal_id  BIGINT       NOT NULL,
  voter        TEXT         NOT NULL,
  options      JSONB        NOT NULL,   -- [{"option":"VOTE_OPTION_YES","weight":"1.000000000000000000"}] as the event encodes it
  option       TEXT,                    -- yes | no | abstain | no_with_veto when one option has weight 1; NULL for weighted votes
  height       BIGINT       NOT NULL,
  tx_index     INTEGER      NOT NULL,
  time         TIMESTAMPTZ  NOT NULL,
  PRIMARY KEY (proposal_id, voter)      -- a later vote replaces the earlier one (ON CONFLICT DO UPDATE when height is greater)
);

CREATE INDEX idx_proposal_votes_recent ON explorer.proposal_votes (proposal_id, height DESC);
CREATE INDEX idx_proposal_votes_voter  ON explorer.proposal_votes (voter, proposal_id DESC);

CREATE TABLE explorer.proposal_deposits (
  proposal_id  BIGINT       NOT NULL,
  depositor    TEXT         NOT NULL,
  height       BIGINT       NOT NULL,
  tx_index     INTEGER      NOT NULL,
  amount       JSONB        NOT NULL,
  time         TIMESTAMPTZ  NOT NULL,
  PRIMARY KEY (proposal_id, depositor, height, tx_index)
);

-- 10. Tokens, IBC channels and the chain registry cache
CREATE TABLE explorer.denoms (
  denom                 TEXT           PRIMARY KEY,  -- base denom as it appears in amounts: ubze, factory/bze1…/uhoney, ibc/HASH, LP denoms
  symbol                TEXT,                        -- display symbol: HONEY; for ibc denoms from registry_assets when bank metadata is empty
  name                  TEXT,
  exponent              SMALLINT       NOT NULL DEFAULT 0,
  description           TEXT,
  kind                  TEXT           NOT NULL,     -- native | factory | ibc | lp | unknown
  origin_chain_id       TEXT,                        -- ibc: counterparty chain of the first hop, via ibc_channels; names and logo via chains
  ibc_base_denom        TEXT,
  ibc_path              TEXT,                        -- transfer/channel-N
  creator               TEXT,                        -- factory
  admin                 TEXT,                        -- factory; NULL = renounced ("supply is fixed")
  created_height        BIGINT,
  created_tx_hash       TEXT,
  created_time          TIMESTAMPTZ,
  logo_url              TEXT,                        -- factory branding, or the registry asset logo for ibc denoms
  website               TEXT,
  supply                NUMERIC(78,0),
  holders_count         INTEGER,                     -- sync time: owners with balance >= 1 display unit (10^exponent base units), recomputed with every holders snapshot
  halted                BOOLEAN        NOT NULL DEFAULT false,  -- tradebin halted store
  markets               TEXT[]         NOT NULL DEFAULT '{}',   -- tradebin market ids the denom trades in
  price_usd             NUMERIC(30,12),
  price_change_24h_pct  NUMERIC(10,4),
  price_updated_at      TIMESTAMPTZ,
  metadata              JSONB,                       -- bank metadata as the node returns it ("raw metadata" in More details)
  updated_at            TIMESTAMPTZ    NOT NULL
);

CREATE INDEX idx_denoms_kind         ON explorer.denoms (kind);
CREATE INDEX idx_denoms_search_trgm  ON explorer.denoms USING GIN ((coalesce(symbol,'') || ' ' || coalesce(name,'')) gin_trgm_ops);

CREATE TABLE explorer.token_holders (            -- snapshot from bank DenomOwners, refreshed per denom by sync-state; every non-zero balance, no threshold
  denom       TEXT           NOT NULL,
  address     TEXT           NOT NULL,
  balance     NUMERIC(78,0)  NOT NULL,
  updated_at  TIMESTAMPTZ    NOT NULL,
  PRIMARY KEY (denom, address)
);

CREATE INDEX idx_token_holders_rank ON explorer.token_holders (denom, balance DESC);

CREATE TABLE explorer.token_events (
  height    BIGINT        NOT NULL,
  tx_index  INTEGER       NOT NULL,
  seq       INTEGER       NOT NULL,
  denom     TEXT          NOT NULL,
  kind      TEXT          NOT NULL,   -- created | minted | burned | admin_changed | metadata_changed | branding_changed | halted | unhalted | market_created | pool_created
  actor     TEXT,
  amount    NUMERIC(78,0),
  details   JSONB,
  time      TIMESTAMPTZ   NOT NULL,
  PRIMARY KEY (height, tx_index, seq)
);

CREATE INDEX idx_token_events_denom ON explorer.token_events (denom, height DESC);

CREATE TABLE explorer.ibc_channels (             -- from the local node: ibc channels + client states
  channel_id               TEXT         PRIMARY KEY,   -- channel-N on port transfer
  port_id                  TEXT         NOT NULL DEFAULT 'transfer',
  client_id                TEXT,
  connection_id            TEXT,
  counterparty_chain_id    TEXT,                       -- joins chains.chain_id
  counterparty_channel_id  TEXT,
  state                    TEXT,
  updated_at               TIMESTAMPTZ  NOT NULL
);

CREATE TABLE explorer.chains (                   -- cache of the Cosmos chain registry: one row per chain we know about
  chain_id              TEXT         PRIMARY KEY,   -- cosmoshub-4, noble-1, osmosis-1, …
  registry_name         TEXT,                       -- directory in the registry: cosmoshub, noble, osmosis
  pretty_name           TEXT,                       -- "Cosmos Hub", shown as the Origin tag and in "cross-chain transfer from …"
  network_type          TEXT,                       -- mainnet | testnet
  bech32_prefix         TEXT,
  logo_url              TEXT,
  explorer_tx_url       TEXT,                       -- the registry's tx_page template
  explorer_account_url  TEXT,                       -- the registry's account_page template: "see the receiver on their explorer"
  registry_fetched_at   TIMESTAMPTZ,                -- NULL = chain id seen on a channel but not (yet) in the registry
  updated_at            TIMESTAMPTZ  NOT NULL
);

CREATE TABLE explorer.registry_assets (          -- cache of the registry assetlist.json of every chain in `chains`
  chain_id      TEXT         NOT NULL,
  base          TEXT         NOT NULL,            -- base denom on that chain: uatom, uusdc, ibc/… for assets it holds via IBC
  symbol        TEXT,
  name          TEXT,
  display       TEXT,
  exponent      SMALLINT,
  logo_url      TEXT,
  coingecko_id  TEXT,
  traces        JSONB,                            -- the registry's trace list, for multi-hop denoms
  updated_at    TIMESTAMPTZ  NOT NULL,
  PRIMARY KEY (chain_id, base)
);

-- 11. Cross-chain (IBC) transfers and their status
CREATE TABLE explorer.ibc_transfers (
  channel_id             TEXT          NOT NULL,   -- BZE-side channel: packet_src_channel (out), packet_dst_channel (in)
  sequence               BIGINT        NOT NULL,   -- packet_sequence
  direction              TEXT          NOT NULL,   -- out | in
  status                 TEXT          NOT NULL,   -- out: pending | delivered | refunded_rejected | refunded_timeout
                                                   -- in:  received | rejected
  sender                 TEXT          NOT NULL,   -- packet data: bze1… (out) or the foreign sender (in)
  receiver               TEXT          NOT NULL,   -- packet data: the foreign receiver (out) or bze1… (in)
  denom                  TEXT          NOT NULL,   -- as sent: the BZE denom (out) / the packet denom before BZE's ibc/ hash (in)
  amount                 NUMERIC(78,0) NOT NULL,
  memo                   TEXT,
  counterparty_chain_id  TEXT,                     -- from ibc_channels at insert
  created_height         BIGINT        NOT NULL,   -- the BZE transaction that created the row: MsgTransfer (out) or MsgRecvPacket (in)
  created_tx_index       INTEGER       NOT NULL,
  created_tx_hash        TEXT          NOT NULL,
  created_time           TIMESTAMPTZ   NOT NULL,
  timeout_height         TEXT,                     -- packet_timeout_height "revision-height"; "0-0" = none
  timeout_time           TIMESTAMPTZ,              -- packet_timeout_timestamp (nanoseconds) converted; NULL = none
  result_height          BIGINT,                   -- out: the acknowledgement or timeout transaction on BZE
  result_tx_hash         TEXT,
  result_time            TIMESTAMPTZ,
  ack_error              TEXT,                     -- the other chain's error text (refunded_rejected) or BZE's reason (in: rejected)
  PRIMARY KEY (channel_id, sequence, direction)
);

CREATE INDEX idx_ibc_transfers_created_tx ON explorer.ibc_transfers (created_tx_hash);
CREATE INDEX idx_ibc_transfers_sender     ON explorer.ibc_transfers (sender,   created_height DESC) WHERE direction = 'out';
CREATE INDEX idx_ibc_transfers_receiver   ON explorer.ibc_transfers (receiver, created_height DESC) WHERE direction = 'in';
CREATE INDEX idx_ibc_transfers_pending    ON explorer.ibc_transfers (created_height) WHERE status = 'pending';

-- 12. Labels
CREATE TABLE explorer.labels (
  address     TEXT         PRIMARY KEY,
  name        TEXT         NOT NULL,     -- "Fee collector", "DEX", "Burner", "Cross-chain escrow (Cosmos Hub)", "BZE Alpha Team treasury"
  kind        TEXT         NOT NULL,     -- module | ibc_escrow | validator_owner | known
  module      TEXT,                      -- module name for kind in (module, ibc_escrow)
  url         TEXT,
  source      TEXT         NOT NULL,     -- seed | sync | manual
  updated_at  TIMESTAMPTZ  NOT NULL
);

-- 13. Parameters
CREATE TABLE explorer.param_snapshots (
  module        TEXT         NOT NULL,   -- staking | mint | distribution | slashing | gov | tradebin | tokenfactory | rewards | burner | cointrunk | txfeecollector
  height        BIGINT       NOT NULL,   -- height at which the change was first observed
  time          TIMESTAMPTZ  NOT NULL,
  params        JSONB        NOT NULL,   -- the full params object as the node returns it
  changed_keys  TEXT[]       NOT NULL,   -- keys that differ from the previous snapshot; '{}' for the first
  proposal_id   BIGINT,                  -- the proposal resolved at or just before this height (gov change or software upgrade), if any
  PRIMARY KEY (module, height)
);

-- 14. Chain state and statistics
CREATE TABLE explorer.chain_state (          -- latest synced state, for the home tiles and the validators header
  key         TEXT         PRIMARY KEY,      -- staking_pool | supply | mint | community_pool | validator_counts | price_ubze | node_status
  value       JSONB        NOT NULL,
  height      BIGINT,
  updated_at  TIMESTAMPTZ  NOT NULL
);

CREATE TABLE explorer.daily_stats (          -- filled per closed day by the state sync's nightly tick; "today" is computed live
  day                DATE     PRIMARY KEY,
  blocks             INTEGER  NOT NULL,
  txs                INTEGER  NOT NULL,
  txs_failed         INTEGER  NOT NULL,
  active_addresses   INTEGER  NOT NULL,
  new_addresses      INTEGER  NOT NULL,
  transfers          INTEGER  NOT NULL,
  fees               JSONB    NOT NULL,
  avg_block_time_ms  INTEGER
);

-- 15. Operational tables
CREATE TABLE explorer.indexer_state (
  key         TEXT         PRIMARY KEY,   -- live_floor | last_indexed_height
  value       TEXT         NOT NULL,
  updated_at  TIMESTAMPTZ  NOT NULL
);
-- live_floor is written once, at the live indexer's first write, and never changed:
-- the live side never indexes below it, the backfill never above it.

CREATE TABLE explorer.index_failures (
  height       BIGINT       NOT NULL,
  source       TEXT         NOT NULL,     -- live | backfill | reindex
  attempts     INTEGER      NOT NULL,
  error        TEXT         NOT NULL,
  failed_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  resolved_at  TIMESTAMPTZ,              -- set by a successful reindex of the height
  PRIMARY KEY (height, source)
);
CREATE INDEX idx_index_failures_open ON explorer.index_failures (failed_at) WHERE resolved_at IS NULL;

CREATE TABLE explorer.backfill_checkpoints (
  job                TEXT         PRIMARY KEY,   -- 'main', or a named reindex run
  ceiling_height     BIGINT       NOT NULL,      -- live_floor - 1 for the main job; the top of a range otherwise
  floor_height       BIGINT       NOT NULL,      -- stop here (inclusive)
  lowest_dispatched  BIGINT       NOT NULL,      -- the checkpoint: dispatch order is monotonic downward
  blocks_done        BIGINT       NOT NULL DEFAULT 0,
  status             TEXT         NOT NULL,      -- running | paused | done | error
  last_error         TEXT,
  started_at         TIMESTAMPTZ  NOT NULL,
  updated_at         TIMESTAMPTZ  NOT NULL
);
-- on restart the dispatcher resumes X + M heights above lowest_dispatched and skips the heights already in explorer.blocks

CREATE TABLE explorer.sync_jobs (
  job              TEXT         PRIMARY KEY,   -- validators | proposals | denoms | holders | ibc_channels | chain_registry | labels | params | prices | chain_state | orders | daily_stats
  last_run_at      TIMESTAMPTZ,
  last_success_at  TIMESTAMPTZ,
  cursor           JSONB,                      -- e.g. the denom being paginated, or the chain ids queued for a registry refetch
  last_error       TEXT
);
-- explorer.schema_migrations is owned by golang-migrate

