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

export interface BlockTx {
  height: number;
  tx_index: number;
  hash: string;
  success: boolean;
  msg_types: string[];
  fee: Coin[];
  signer: string | null;
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
  transfers: Transfer[];
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
}

export interface Message {
  msg_index: number;
  type_url: string;
  sender: string | null;
  module: string | null;
  body: Record<string, unknown> | null;
  events: unknown[] | null;
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
