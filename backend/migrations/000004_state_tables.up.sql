-- The unpartitioned tables: accounts, the classification mirror, DEX orders,
-- validators, governance, tokens, IBC, labels, parameters and chain state
-- (sections 5 and 7 to 14 of the schema design).

-- 5. Accounts, activity and the classification tables
CREATE TABLE explorer.accounts (
  address            TEXT         NOT NULL PRIMARY KEY,
  first_seen_height  BIGINT       NOT NULL,
  first_seen_time    TIMESTAMPTZ  NOT NULL,
  last_seen_height   BIGINT       NOT NULL,
  tx_count           BIGINT       NOT NULL DEFAULT 0,   -- transactions signed by the address
  activity_count     BIGINT       NOT NULL DEFAULT 0    -- account_activity rows
);

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
