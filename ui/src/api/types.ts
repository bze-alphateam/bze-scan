// Shapes of the backend API (`backend/README.md`, "HTTP API"). Amounts and
// other big decimals are strings in base units, times RFC 3339 strings in UTC,
// and an absent value is null, never a missing field.

/** A list page: `next_cursor` is null on the last page. */
export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

/** Paging of every list route. `limit` is 1 to 100 (the API defaults to 25). */
export interface PageParams {
  cursor?: string;
  limit?: number;
}

/** `/blocks`: a page, or with `before` the blocks below that height (exclusive), newest first. */
export interface BlockListParams extends PageParams {
  before?: number;
}

/** The error envelope every failed request answers with. */
export interface ErrorBody {
  error: { code: string; message: string };
}

export interface Coin {
  denom: string;
  amount: string;
}

/** A coin with its display data when the denom is known. */
export interface Balance extends Coin {
  symbol: string | null;
  exponent: number | null;
}

/** Labels: `module` accounts, `ibc_escrow`, `validator_owner`, `known`. */
export interface Label {
  name: string;
  kind: "module" | "ibc_escrow" | "validator_owner" | "known" | (string & {});
}

// Blocks

export interface Proposer {
  operator_address: string;
  moniker: string;
}

export interface BlockSummary {
  height: number;
  time: string;
  hash: string;
  tx_count: number;
  tx_failed_count: number;
  proposer_cons_address: string | null;
  block_time_ms: number | null;
  size_bytes: number | null;
  proposer: Proposer | null;
}

/** A transaction's first message (`msg_index` 0), for one-line summaries in lists. */
export interface FirstMessage {
  type_url: string;
  module: string | null;
  body: Record<string, unknown> | null;
}

export interface BlockTx {
  height: number;
  tx_index: number;
  hash: string;
  success: boolean;
  msg_types: string[];
  fee: Coin[];
  signer: string | null;
  gas_wanted: number | null;
  gas_used: number | null;
  first_message: FirstMessage | null;
}

export interface Transfer {
  seq: number;
  msg_index: number | null;
  kind: "transfer" | "mint" | "burn";
  sender: string | null;
  sender_label: Label | null;
  recipient: string | null;
  recipient_label: Label | null;
  denom: string;
  amount: string;
  symbol: string | null;
  exponent: number | null;
}

/** An item of `/blocks/{height}/transfers`: a transfer with its transaction, `tx_index` -1 and `tx_hash` null for the block's own. */
export interface BlockTransfer extends Transfer {
  tx_index: number;
  tx_hash: string | null;
}

export interface BlockEvent {
  seq: number;
  type: string;
  attrs: Record<string, unknown>;
}

export interface Block extends BlockSummary {
  minted: string | null;
  inflation: string | null;
  fees_distributed: Coin[] | null;
  signatures_count: number | null;
  signatures_power_pct: string | null;
  transactions: BlockTx[];
  /** The block's own transfers (`msg_index` null). */
  transfers: Transfer[];
  /** Every transfer at the height, the transactions' included. */
  transfers_count: number;
  events: BlockEvent[];
  events_next_cursor: string | null;
}

// Transactions

export type TxStatusFilter = "success" | "failed";

export interface TxListParams extends PageParams {
  status?: TxStatusFilter;
}

export interface TxSummary {
  height: number;
  tx_index: number;
  hash: string;
  time: string;
  success: boolean;
  msg_count: number;
  msg_types: string[];
  fee: Coin[];
  signer: string | null;
  first_message: FirstMessage | null;
}

/** An event of a message: SDK attribute values are strings, typed (`bze.*`) ones decoded JSON. */
export interface MessageEvent {
  type: string;
  attrs: Record<string, unknown>;
}

export interface Message {
  msg_index: number;
  type_url: string;
  sender: string | null;
  module: string | null;
  /** The message as proto JSON (snake_case fields); null for a type the backend cannot decode. */
  body: Record<string, unknown> | null;
  /** Null for a failed transaction's messages. */
  events: MessageEvent[] | null;
}

export interface Order {
  order_id: string;
  market_id: string;
  owner: string;
  side: string;
  price: string;
  amount: string;
  filled_amount: string;
  status: "open" | "filled" | "cancelled";
  created_height: number;
  created_time: string;
  created_tx_hash: string | null;
  message_id: string | null;
  closed_height: number | null;
  closed_time: string | null;
}

export interface Fill {
  height: number;
  time: string;
  order_id: string;
  market_id: string;
  side: string;
  price: string;
  amount: string;
  maker: string;
  taker: string;
  taker_tx_hash: string | null;
  taker_message_id: string | null;
}

export interface TxDex {
  placed: Order[];
  fills: Fill[];
  cancelled: Order[];
  pending: boolean;
}

export interface CounterpartyChain {
  chain_id: string | null;
  pretty_name: string | null;
  logo_url: string | null;
  explorer_account_url: string | null;
  explorer_tx_url: string | null;
}

export interface IbcTransfer {
  channel_id: string;
  sequence: number;
  direction: "out" | "in";
  status: "pending" | "delivered" | "refunded_rejected" | "refunded_timeout" | "received" | "rejected";
  expired: boolean;
  sender: string;
  receiver: string;
  denom: string;
  amount: string;
  memo: string | null;
  counterparty: CounterpartyChain;
  created_height: number;
  created_tx_hash: string;
  created_time: string;
  timeout_height: string | null;
  timeout_time: string | null;
  result_height: number | null;
  result_tx_hash: string | null;
  result_time: string | null;
  ack_error: string | null;
}

export interface Tx extends TxSummary {
  code: number;
  codespace: string | null;
  error_log: string | null;
  gas_wanted: number | null;
  gas_used: number | null;
  fee_payer: string | null;
  signers: string[];
  memo: string | null;
  messages: Message[];
  transfers: Transfer[];
  dex: TxDex | null;
  ibc_transfers: IbcTransfer[] | null;
}

// Validators

export type ValidatorStatus = "bonded" | "unbonding" | "unbonded";

export interface ValidatorListParams extends PageParams {
  status?: ValidatorStatus | "all";
}

export interface Validator {
  rank: number | null;
  moniker: string;
  operator_address: string;
  tokens: string;
  voting_power_pct: string | null;
  commission_rate: string;
  uptime: string | null;
  jailed: boolean;
  status: ValidatorStatus;
  /** The Keybase picture of the validator's identity; null without one. */
  logo_url: string | null;
  /** Blocks missed in the last `signed_blocks_window` blocks (the slashing module's signing info); null until known. */
  missed_blocks: number | null;
  signed_blocks_window: number | null;
  /** Of the last `of` proposals whose voting ended (ten, fewer while the chain has fewer), how many it voted on. */
  recent_votes: { voted: number; of: number };
  /** An active validator that missed the last 10 blocks or more in a row; null outside the active set or while unknown. */
  offline: boolean | null;
  /** How many blocks in a row up to the latest indexed one an active validator did not sign; null like `offline`. */
  missed_in_a_row: number | null;
  /** The highest block it was seen signing; null when none since the explorer watches signatures. */
  last_signed_height: number | null;
}

export interface ValidatorsSummary {
  bonded_tokens: string | null;
  staked_share: string | null;
  reward_rate: string | null;
  unbonding_period: number | null;
  top5_share: string | null;
  max_validators: number | null;
}

export interface ValidatorList extends Page<Validator> {
  summary: ValidatorsSummary | null;
}

/** The Hall of Fame rule as the API states it: a vote on each of the last `proposals` closed proposals, at most `max_missed_blocks` missed of the last `signed_blocks_window` blocks (null until known). */
export interface HallOfFameRules {
  proposals: number;
  max_missed_blocks: number;
  signed_blocks_window: number | null;
}

/** `GET /v1/validators/hall-of-fame`: the members, the smallest first, in one page, and the rule. */
export interface HallOfFame extends Page<Validator> {
  rules: HallOfFameRules;
}

export interface ValidatorEvent {
  height: number;
  tx_index: number;
  tx_hash: string | null;
  kind: string;
  details: Record<string, unknown> | null;
  time: string;
}

export interface ValidatorVote {
  proposal_id: number;
  title: string | null;
  option: string | null;
  options: unknown;
  height: number;
  time: string;
}

export interface ValidatorDetail extends Validator {
  account_address: string;
  consensus_address: string | null;
  consensus_pubkey: string | null;
  identity: string | null;
  website: string | null;
  security_contact: string | null;
  details: string | null;
  tombstoned: boolean;
  jailed_until: string | null;
  delegator_shares: string;
  commission_max_rate: string;
  commission_max_change_rate: string;
  commission_update_time: string | null;
  min_self_delegation: string | null;
  self_delegation: string | null;
  delegator_count: number | null;
  missed_blocks: number | null;
  signed_blocks_window: number | null;
  first_seen_height: number | null;
  first_seen_time: string | null;
  updated_at: string;
  recent_blocks: BlockSummary[];
  events: ValidatorEvent[];
  votes: ValidatorVote[];
}

// Accounts

export interface AccountDelegation {
  validator: string;
  moniker: string | null;
  amount: string;
}

export interface AccountUnbonding extends AccountDelegation {
  completion_time: string;
}

export interface AccountReward {
  validator: string;
  coins: Coin[];
}

export interface Account {
  address: string;
  label: Label | null;
  first_seen: { height: number; time: string } | null;
  last_seen_height: number | null;
  tx_count: number;
  activity_count: number;
  balances: Balance[];
  delegations: AccountDelegation[];
  unbonding: AccountUnbonding[];
  rewards: AccountReward[];
  total_staked: string | null;
  total_rewards: Coin[];
  live: { available: boolean };
}

/** What the account's open orders on one market lock of an asset: its sells under the base denom, its buys under the quote. */
export interface OpenOrdersMarket {
  market_id: string;
  base: string;
  quote: string;
  /** Base units of the asset. */
  amount: string;
  orders_count: number;
}

/** What the account's open orders lock of one denom, per market (largest first). */
export interface OpenOrdersAsset {
  denom: string;
  amount: string;
  orders_count: number;
  markets: OpenOrdersMarket[];
}

/** `/accounts/{address}/open-orders`: the tokens locked in the account's open DEX orders. */
export interface OpenOrders {
  /** "index" or "node": for debugging, the UI ignores it. */
  source: string;
  height: number;
  orders_count: number;
  markets_count: number;
  assets: OpenOrdersAsset[];
}

/** The activity list's category filters. */
export type ActivityCategory =
  | "sent"
  | "received"
  | "staking"
  | "dex"
  | "governance"
  | "tokens"
  | "rewards"
  | "burner"
  | "cross_chain"
  | "other";

export interface ActivityParams extends PageParams {
  category?: ActivityCategory;
  /** A numbered page (1 = newest), `limit` rows each; the answer then carries `total`. */
  page?: number;
}

/** A numbered page of activity: `total` counts the address's rows (of the category, with one). */
export interface ActivityPage extends Page<Activity> {
  total: number;
}

/** The other party of an activity row: an account, a validator operator (with its moniker) or a module account (with its label). */
export interface ActivityCounterparty {
  address: string;
  label: Label | null;
  moniker: string | null;
}

/** A typed event naming the address, on a block-level row: attribute values decoded (strings, or arrays for repeated fields). */
export interface ActivityDetail {
  type: string;
  attrs: Record<string, unknown>;
}

/** What one transaction (`tx_hash` set) or one block's own events (`tx_hash` null) did to an address. */
export interface Activity {
  time: string;
  height: number;
  tx_hash: string | null;
  /** The classification's kind (`send`, `dex_fill`, …): `backend/internal/classify/classify.go`. */
  kind: string;
  category: ActivityCategory;
  is_signer: boolean;
  success: boolean;
  /** Null unless there is exactly one other party. */
  counterparty: ActivityCounterparty | null;
  /** The net balance change per denom, the fee included. */
  deltas: Balance[];
  msg_types: string[];
  /** Block-level rows only; null on transaction rows. */
  details: ActivityDetail[] | null;
  /** The transaction's fee when the address signed or paid it; null otherwise. */
  fee: Balance[] | null;
}

export interface IbcListParams extends PageParams {
  direction?: "in" | "out";
}

// Tokens

export type TokenKind = "native" | "factory" | "ibc" | "lp" | "unknown";

export interface TokenListParams extends PageParams {
  kind?: TokenKind;
}

export interface OriginChain {
  chain_id: string;
  name: string | null;
  logo_url: string | null;
  /** The chain registry's status: live, upcoming, or killed (the chain was shut down); null when unknown. */
  status: string | null;
}

/** BZE's channel of an IBC denom's first hop: its state (open, closed…) and its light client's status (active, expired, frozen). */
export interface TokenIbcChannel {
  channel_id: string;
  state: string | null;
  client_status: string | null;
}

export interface TokenSummary {
  denom: string;
  symbol: string | null;
  name: string | null;
  kind: TokenKind | (string & {});
  exponent: number;
  supply: string | null;
  holders_count: number | null;
  price_usd: string | null;
  price_change_24h_pct: string | null;
  halted: boolean;
  logo_url: string | null;
  origin_chain: OriginChain | null;
  /** The block time of the denom's `created` token event; null without one. */
  created_time: string | null;
  /** The port/channel pairs of an IBC denom's path; null for the other kinds. */
  ibc_hops: number | null;
  /** The chain of the first hop of a multi-hop IBC denom (it came through it); null otherwise. */
  via_chain: OriginChain | null;
  /** The two assets of a liquidity-pool share; null for the other kinds. */
  lp_pair: LpPair | null;
  /** BZE's channel of an IBC denom's first hop; null for the other kinds and until known. */
  ibc_channel: TokenIbcChannel | null;
  /** An IBC denom that can no longer travel back: closed channel, expired or frozen client, or a shut-down chain. */
  stranded: boolean;
}

export interface LpPair {
  base: string;
  quote: string;
}

/** A module account: GET /api/v1/modules. */
export interface ModuleAccount {
  address: string;
  name: string;
  module: string;
}

/** What moved of a denom in a window: `amount` summed in base units. */
export interface TransferVolume {
  count: number;
  amount: string;
}

/** One event in a denom's history; `tx_hash` is null for a block-level halt. */
export interface TokenEvent {
  height: number;
  tx_index: number;
  seq: number;
  tx_hash: string | null;
  kind:
    | "created"
    | "minted"
    | "burned"
    | "admin_changed"
    | "metadata_changed"
    | "branding_changed"
    | "market_created"
    | "pool_created"
    | "halted"
    | "unhalted"
    | (string & {});
  actor: string | null;
  actor_label: Label | null;
  amount: string | null;
  details: Record<string, unknown> | null;
  time: string;
}

/** The token page: the list item plus its origin, first sight, admin, markets, metadata and newest events. */
export interface Token extends TokenSummary {
  description: string | null;
  origin_chain_id: string | null;
  ibc_base_denom: string | null;
  ibc_path: string | null;
  creator: string | null;
  creator_label: Label | null;
  /** Null once renounced (and for every denom that is not a factory one). */
  admin: string | null;
  admin_label: Label | null;
  created_height: number | null;
  created_tx_hash: string | null;
  website: string | null;
  /** Tradebin market ids, `base/quote`. */
  markets: string[];
  price_updated_at: string | null;
  metadata: unknown;
  updated_at: string;
  /** The newest 20 events, newest first. */
  events: TokenEvent[];
  events_next_cursor: string | null;
  transfers_24h: TransferVolume;
}

/** A holder in the last snapshot; `share_pct` of the supply, null without one. */
export interface TokenHolder {
  rank: number;
  address: string;
  label: Label | null;
  balance: string;
  share_pct: string | null;
}

/** One move of a denom, with where it happened. */
export interface TokenTransfer extends Transfer {
  height: number;
  tx_index: number;
  tx_hash: string | null;
  time: string;
}

// Proposals

export type ProposalKind =
  | "software_upgrade"
  | "community_pool_spend"
  | "parameter_change"
  | "cointrunk_publisher"
  | "ibc_client_update"
  | "text"
  | "other";

export type ProposalStatus = "deposit_period" | "voting_period" | "passed" | "rejected" | "failed" | "canceled" | "dropped";

export interface ProposalListParams extends PageParams {
  status?: ProposalStatus;
}

/** A proposal's vote count in base units of the bond denom. */
export interface Tally {
  yes: string;
  no: string;
  abstain: string;
  no_with_veto: string;
}

export interface ProposalSummary {
  id: number;
  title: string;
  kind: ProposalKind | (string & {});
  status: ProposalStatus;
  expedited: boolean;
  submit_time: string;
  deposit_end_time: string | null;
  voting_start_time: string | null;
  voting_end_time: string | null;
  /** Null until the state sync tallies the proposal. */
  tally: Tally | null;
  /** The share of the bonded tokens that voted, percent with five decimals. */
  turnout_pct: string | null;
  /**
   * Where the bonded tokens behind the turnout come from: `sync` while the
   * proposal voted, `archive` read after the fact by the snapshots command
   * (historical); null without them.
   */
  tally_source: "sync" | "archive" | null;
}

export interface Proposal extends ProposalSummary {
  summary: string | null;
  metadata: string | null;
  proposer: string | null;
  proposer_label: Label | null;
  message_types: string[];
  /** Proto JSON; a legacy content inside its `MsgExecLegacyContent`. */
  messages: Record<string, unknown>[] | null;
  total_deposit: Coin[] | null;
  tally_bonded_tokens: string | null;
  tally_updated_at: string | null;
  submit_height: number | null;
  submit_tx_hash: string | null;
  resolved_height: number | null;
  /** Bonded validators whose owner voted, of all bonded validators. */
  /** Validators whose owner voted, whatever their status today; `total` is the active set while voting, null once voting ended. */
  validators_voted: { voted: number; total: number | null };
  /** The accounts that voted (one vote each, the latest). */
  votes_count: number;
  /**
   * The validator set the tally counted (the CometBFT set at `height`, the
   * validators jailed in the tally's block left out): `size` validators, of
   * which `non_voters_count` did not vote. Null until the state sync recorded
   * it, and while voting is open.
   */
  active_set: ActiveSet | null;
  updated_at: string;
}

export interface ActiveSet {
  height: number;
  size: number;
  non_voters_count: number;
}

/** A validator of a closed proposal's tally set that did not vote, largest power first. */
export interface ProposalNonVoter {
  /** Null (and `moniker` too) when no validator is known with the consensus address. */
  operator_address: string | null;
  /** Upper-case hex. */
  consensus_address: string;
  moniker: string | null;
  voting_power: number;
  /** Its share of the set's power, percent with five decimals. */
  power_pct: string;
}

export type VoteOption = "yes" | "no" | "abstain" | "no_with_veto";

export interface VoteListParams extends PageParams {
  /** A single option, or `weighted` for the split votes. */
  option?: VoteOption | "weighted";
  /** `validators`: the votes of accounts owning a validator. */
  voter?: "validators";
}

/** Where a vote or deposit row comes from: an indexed transaction, or the archive's state before the tally. */
export type RowSource = "event" | "archive";

export interface ProposalVote {
  voter: string;
  voter_label: Label | null;
  /** Null for a split vote (`options` holds the weights). */
  option: VoteOption | null;
  options: unknown;
  height: number;
  tx_index: number;
  tx_hash: string | null;
  time: string;
  /** The validator the voter owns, with its current share; null for other voters. */
  validator: string | null;
  validator_operator: string | null;
  voting_power_pct: string | null;
  /** `archive`: read from the state before the tally (historical: tx_index -1, no tx_hash, the tally block's time). */
  source: RowSource;
}

export interface ProposalDeposit {
  depositor: string;
  depositor_label: Label | null;
  amount: Coin[] | null;
  height: number;
  tx_index: number;
  tx_hash: string | null;
  time: string;
  /** `archive`: the depositor's total read from the state before the tally (historical, no tx_hash). */
  source: RowSource;
}

// Statistics

/** One stored UTC day of `/stats`. */
export interface StatsDay {
  day: string;
  blocks: number;
  txs: number;
  txs_failed: number;
  active_addresses: number;
  new_addresses: number;
  transfers: number;
  fees: string;
  avg_block_time_ms: number | null;
}

/** The average block time from `from_height` to the latest indexed block; null when `from_height` is not indexed. */
export interface BlockTimeWindow {
  blocks: number;
  from_height: number;
  avg_block_time_ms: number | null;
}

/** `GET /stats`: the home tiles; every chain-state field is null until its row exists. */
export interface Stats {
  latest_height: number | null;
  latest_time: string | null;
  avg_block_time_ms: number | null;
  /** Over the last 10k, 30k, 50k and 100k blocks, in this order; empty before the first block. */
  block_time_windows: BlockTimeWindow[];
  txs_24h: number | null;
  bonded_tokens: string | null;
  supply: string | null;
  community_pool: string | null;
  staked_share: string | null;
  inflation: string | null;
  reward_rate: string | null;
  validators: { active: number; total: number } | null;
  max_validators: number | null;
  unbonding_period: number | null;
  price_usd: string | null;
  price_change_24h_pct: string | null;
  daily: StatsDay[];
  today: StatsDay | null;
}

// Search

export interface SearchResult {
  type: "block" | "transaction" | "account" | "validator" | (string & {});
  id: string;
  label: string;
  /** Accounts only: whether the explorer has seen the address. */
  indexed?: boolean;
}

export interface SearchResponse {
  results: SearchResult[];
}

// Status

export interface Status {
  live_fill: {
    healthy: boolean;
    checked_at: string | null;
    db_height: number | null;
    node_height: number | null;
    archive_height: number | null;
  };
  back_fill: {
    status: "finished" | "in_progress";
    oldest_height: number | null;
  };
}

// Parameters

/** A module's live parameters (proto JSON as the node answers) and its latest recorded change. */
export interface ModuleParams {
  params: Record<string, unknown>;
  last_change: { height: number; time: string; proposal_id: number | null } | null;
}

/** `GET /params`: by module name; a module the node failed to answer is absent. */
export type ChainParams = Partial<Record<string, ModuleParams>>;

// Raw JSON proxied from archive nodes

/** A node's JSON-RPC response body, verbatim. */
export type RawRpcResponse = Record<string, unknown>;

export interface RawTx {
  height: number;
  index: number;
  /** The base64 transaction. */
  tx: string;
  tx_result: Record<string, unknown>;
}
