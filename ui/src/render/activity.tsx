import BigNumber from "bignumber.js";

import type { Activity, ActivityCategory, ActivityCounterparty, ActivityDetail, Balance, Coin, IbcTransfer } from "@/api/types";
import type { ActionCategory } from "@/components/ui/action-card";
import { formatAmount, shortAddress } from "@/lib/format";
import { coinText, humanise, parseCoins } from "@/render/messages/common";
import { marketName, quoteAmount, splitMarket } from "@/render/messages/dex";
import type { RenderContext } from "@/render/messages/types";

// The account's activity rows in plain words, from what the row carries
// (kind, counterparty, deltas, details, msg_types) and nothing else: the UI
// never fetches the transaction to render a row. Pure, so it is tested
// without rendering.

/** The category chips and the `category` each asks the list for. */
export const ACTIVITY_FILTERS: { label: string; value: ActivityCategory | null }[] = [
  { label: "All", value: null },
  { label: "Sent", value: "sent" },
  { label: "Received", value: "received" },
  { label: "Staking", value: "staking" },
  { label: "DEX", value: "dex" },
  { label: "Rewards", value: "rewards" },
  { label: "Governance", value: "governance" },
  { label: "Tokens", value: "tokens" },
  { label: "Burner", value: "burner" },
  { label: "Cross-chain", value: "cross_chain" },
  { label: "Other", value: "other" },
];

/** The category a `?category=` value names; null (All) for anything else. */
export function parseCategory(value: string | string[] | undefined | null): ActivityCategory | null {
  return ACTIVITY_FILTERS.find((f) => f.value !== null && f.value === value)?.value ?? null;
}

/** The icon of a row: sent and received share the send arrow. */
export function actionCategory(category: ActivityCategory): ActionCategory {
  return category === "sent" || category === "received" ? "send" : category;
}

/** A piece of a sentence: words, or the row's counterparty as a chip. */
export type Part = string | { party: ActivityCounterparty };

/** A chip after the sentence: a cross-chain status, "Failed". */
export interface StatusChip {
  label: string;
  palette: "orange" | "green" | "red";
}

export interface ActivityLine {
  parts: Part[];
  chips: StatusChip[];
  /** True when the kind is unknown and the sentence is the first message type in words. */
  fallback: boolean;
}

/** A counterparty's name: its label, its moniker, else its short address. */
export function partyName(p: ActivityCounterparty): string {
  return p.label?.name ?? p.moniker ?? shortAddress(p.address);
}

/** The sentence as text, the counterparty by name. */
export function lineText(line: Pick<ActivityLine, "parts">): string {
  return line.parts.map((p) => (typeof p === "string" ? p : partyName(p.party))).join("");
}

// Amounts.

/** A balance in display units: its own symbol and exponent, else the context's, else the raw amount and denom. */
export function balanceText(b: Coin & Partial<Pick<Balance, "symbol" | "exponent">>, ctx: RenderContext): string {
  if (b.symbol != null && b.exponent != null) return formatAmount(b.amount, b.exponent, b.symbol);
  return coinText(b, ctx);
}

/** The deltas without the fee (the fee is part of them): what the action itself moved, zero entries dropped. */
export function withoutFee(row: Pick<Activity, "deltas" | "fee">): Balance[] {
  const fee = new Map((row.fee ?? []).map((f) => [f.denom, f.amount]));
  const out: Balance[] = [];
  const seen = new Set<string>();
  for (const d of row.deltas) {
    seen.add(d.denom);
    const amount = new BigNumber(d.amount).plus(fee.get(d.denom) ?? 0);
    if (!amount.isZero()) out.push({ ...d, amount: amount.toFixed() });
  }
  // A fee in a denom the deltas do not hold was paid back by the same transaction.
  for (const f of row.fee ?? []) {
    if (!seen.has(f.denom)) out.push({ ...f, amount: new BigNumber(f.amount).toFixed() });
  }
  return out;
}

const isPositive = (b: Coin) => new BigNumber(b.amount).gt(0);
const abs = (b: Balance): Balance => ({ ...b, amount: new BigNumber(b.amount).abs().toFixed() });

/** One line of the amounts column. */
export interface DeltaLine {
  text: string;
  positive: boolean;
  fee: boolean;
}

/**
 * The amounts column: "+ 2.56 USDC" / "− 10 BZE" per denom, the fee apart as
 * "− 0.002 BZE fee" when the row carries one. Empty when nothing moved.
 */
export function deltaLines(row: Pick<Activity, "deltas" | "fee">, ctx: RenderContext): DeltaLine[] {
  const lines = withoutFee(row).map((b) => {
    const positive = isPositive(b);
    return { text: `${positive ? "+" : "−"} ${balanceText(abs(b), ctx)}`, positive, fee: false };
  });
  for (const f of row.fee ?? []) {
    if (new BigNumber(f.amount).gt(0)) lines.push({ text: `− ${balanceText(f, ctx)} fee`, positive: false, fee: true });
  }
  return lines;
}

// Cross-chain decoration.

const sameHash = (a: string | null, b: string | null) => a != null && b != null && a.toUpperCase() === b.toUpperCase();

/**
 * The cross-chain transfer of a row, from the account's transfers: a sent
 * transfer by the transaction that created it, a received one by the
 * transaction that brought it in, a refund by the transaction that settled
 * it. Null for other kinds or when the transfer is not among them.
 */
export function ibcFor(row: Pick<Activity, "kind" | "tx_hash">, transfers: IbcTransfer[]): IbcTransfer | null {
  if (!row.tx_hash) return null;
  switch (row.kind) {
    case "ibc_out":
      return transfers.find((t) => t.direction === "out" && sameHash(t.created_tx_hash, row.tx_hash)) ?? null;
    case "ibc_in":
      return transfers.find((t) => t.direction === "in" && sameHash(t.created_tx_hash, row.tx_hash)) ?? null;
    case "ibc_refund":
      return transfers.find((t) => t.direction === "out" && sameHash(t.result_tx_hash, row.tx_hash)) ?? null;
    default:
      return null;
  }
}

/** A transfer's status chip: Pending, Expired, Delivered, Refunded; Received or Rejected inbound. */
export function ibcChip(t: IbcTransfer): StatusChip {
  switch (t.status) {
    case "pending":
      return t.expired ? { label: "Expired", palette: "orange" } : { label: "Pending", palette: "orange" };
    case "delivered":
      return { label: "Delivered", palette: "green" };
    case "refunded_rejected":
    case "refunded_timeout":
      return { label: "Refunded", palette: "red" };
    case "received":
      return { label: "Received", palette: "green" };
    case "rejected":
      return { label: "Rejected", palette: "red" };
  }
}

const otherChain = (t: IbcTransfer | null) => t?.counterparty.pretty_name ?? t?.counterparty.chain_id ?? null;

// Block-level details.

const attr = (d: ActivityDetail, key: string): string | null => {
  const v = d.attrs[key];
  return typeof v === "string" && v !== "" ? v : typeof v === "number" ? String(v) : null;
};

const detailsOf = (row: Pick<Activity, "details">, type: string) => (row.details ?? []).filter((d) => d.type === type);

/** The fills naming the address, summed per market and side: "sold 200 BZE for 2.56 USDC on BZE/USDC". */
function fillPhrases(row: Activity, address: string, ctx: RenderContext): string[] {
  const groups = new Map<string, { market: string; side: "buy" | "sell"; base: BigNumber; quote: BigNumber }>();
  for (const d of detailsOf(row, "bze.tradebin.OrderExecutedEvent")) {
    const market = attr(d, "market_id");
    const resting = attr(d, "order_type");
    const amount = attr(d, "amount");
    if (!market || (resting !== "buy" && resting !== "sell") || !amount) continue;
    // The event's side is the resting (maker) order's; the taker did the opposite.
    const side = attr(d, "maker") === address ? resting : resting === "buy" ? "sell" : "buy";
    const key = `${market} ${side}`;
    const g = groups.get(key) ?? { market, side, base: new BigNumber(0), quote: new BigNumber(0) };
    g.base = g.base.plus(amount);
    g.quote = g.quote.plus(quoteAmount(amount, attr(d, "price")) ?? 0);
    groups.set(key, g);
  }
  return [...groups.values()].flatMap((g) => {
    const m = splitMarket(g.market);
    if (!m) return [];
    const base = coinText({ denom: m.base, amount: g.base.toFixed() }, ctx);
    const quote = coinText({ denom: m.quote, amount: g.quote.toFixed() }, ctx);
    return [`${g.side === "sell" ? "sold" : "bought"} ${base} for ${quote} on ${marketName(g.market, ctx)}`];
  });
}

/** The first event of `type` naming the address in `key`, else the first of the type. */
function ownEvent(row: Activity, type: string, key: string, address: string): ActivityDetail | null {
  const all = detailsOf(row, type);
  return all.find((d) => attr(d, key) === address) ?? all[0] ?? null;
}

// Sentences.

interface Input {
  row: Activity;
  address: string;
  ctx: RenderContext;
  ibc: IbcTransfer | null;
  /** The counterparty as a chip part; null when the row has none. */
  party: Part | null;
  /** What the action moved, the fee left out. */
  gains: Balance[];
  losses: Balance[];
}

type Sentence = (i: Input) => Part[];

/** `[...words, " ", chip]` when there is a counterparty, else the alternative words. */
const withParty = (i: Input, before: string, otherwise: string, after = ""): Part[] =>
  i.party ? [before ? `${before} ` : "", i.party, after] : [otherwise];

const by = (signer: Sentence, participant: Sentence): Sentence => (i) => (i.row.is_signer ? signer(i) : participant(i));

const amounts = (list: (Coin & Partial<Pick<Balance, "symbol" | "exponent">>)[], ctx: RenderContext) => list.map((b) => balanceText(b, ctx)).join(" and ");

/** The first message type among `types`, for kinds several message types share. */
const firstType = (row: Activity, ...names: string[]): string | null =>
  row.msg_types.map((t) => t.split(".").pop() ?? t).find((n) => names.includes(n)) ?? null;

const SENTENCES: Record<string, Sentence> = {
  send: by(
    (i) => withParty(i, "Sent to", "Sent tokens to several accounts"),
    (i) => withParty(i, "Received from", "Received tokens"),
  ),
  nft_send: by(
    (i) => withParty(i, "Sent an NFT to", "Sent an NFT"),
    (i) => withParty(i, "Received an NFT from", "Received an NFT"),
  ),
  vesting_create: by(
    (i) => withParty(i, "Created a vesting account for", "Created a vesting account"),
    (i) => withParty(i, "Got a vesting account from", "Got a vesting account"),
  ),

  delegate: (i) => withParty(i, "Staked with", "Staked"),
  undelegate: (i) => withParty(i, "Unstaked from", "Unstaked"),
  redelegate: (i) => withParty(i, "Moved stake to", "Moved stake to another validator"),
  cancel_unbonding: (i) => withParty(i, "Cancelled unstaking from", "Cancelled unstaking"),
  validator: ({ row }) => {
    switch (firstType(row, "MsgCreateValidator", "MsgEditValidator", "MsgUnjail")) {
      case "MsgCreateValidator":
        return ["Created a validator"];
      case "MsgUnjail":
        return ["Unjailed a validator"];
      default:
        return ["Edited a validator"];
    }
  },
  claim_rewards: (i) => {
    const n = i.row.msg_types.filter((t) => t.endsWith(".MsgWithdrawDelegatorReward")).length;
    if (n > 1) return [`Claimed rewards from ${n} validators`];
    return withParty(i, "Claimed rewards from", "Claimed staking rewards");
  },
  claim_commission: () => ["Claimed validator commission"],
  withdraw_address: (i) => withParty(i, "Sent future staking rewards to", "Changed where staking rewards go"),
  community_pool_fund: () => ["Funded the community pool"],
  validator_rewards_deposit: (i) => withParty(i, "Added to the rewards of", "Added to a validator's rewards"),
  unbonding_completed: (i) => {
    const d = ownEvent(i.row, "complete_unbonding", "delegator", i.address);
    const back = d ? parseCoins(attr(d, "amount")) : [];
    return [back.length ? `Unstaking completed · ${amounts(back, i.ctx)} back in your account` : "Unstaking completed · back in your account"];
  },
  redelegation_completed: () => ["Stake move completed"],
  slashed: () => ["A validator was slashed"],

  proposal: () => ["Submitted a governance proposal"],
  proposal_deposit: () => ["Deposited on a governance proposal"],
  // The proposal id is no counterparty (the backend names accounts only); the option is in the transaction.
  vote: () => ["Voted on a governance proposal"],
  proposal_cancel: () => ["Cancelled a governance proposal"],
  proposal_resolved: () => ["A governance proposal's voting ended"],
  proposal_dropped: () => ["A governance proposal expired without enough deposit"],

  authz_exec: by(
    () => ["Acted for another account"],
    (i) => withParty(i, "", "Another account acted for this one", " acted for this account"),
  ),
  authz_grant: by(
    (i) => withParty(i, "Let", "Let another account act for this one", " act for this account"),
    (i) => withParty(i, "Allowed to act for", "Allowed to act for another account"),
  ),
  authz_revoke: by(
    (i) => withParty(i, "Took back", "Took back a permission to act for this account", "'s permission to act for this account"),
    (i) => withParty(i, "No longer allowed to act for", "No longer allowed to act for another account"),
  ),
  feegrant_grant: by(
    (i) => withParty(i, "Offered to pay the fees of", "Offered to pay another account's fees"),
    (i) => withParty(i, "Fees paid from now on by", "Another account offered to pay the fees"),
  ),
  feegrant_revoke: by(
    (i) => withParty(i, "Stopped paying the fees of", "Stopped paying another account's fees"),
    (i) => withParty(i, "Fees no longer paid by", "Another account stopped paying the fees"),
  ),
  feegrant_prune: () => ["Cleared expired fee allowances"],
  group: () => ["Group action"],
  evidence: () => ["Reported a validator's misbehaviour"],
  epoch: () => ["A new epoch began"],

  ibc_out: ({ ibc }) => [`Cross-chain transfer to ${otherChain(ibc) ?? "another chain"}`],
  ibc_in: (i) => {
    const chain = otherChain(i.ibc);
    if (chain) return [`Cross-chain transfer from ${chain}`];
    return withParty(i, "Cross-chain transfer from", "Cross-chain transfer from another chain");
  },
  ibc_refund: ({ ibc }) => {
    const chain = otherChain(ibc);
    return [chain ? `Refunded cross-chain transfer to ${chain}` : "Refunded cross-chain transfer"];
  },
  ibc_fee: () => ["Paid a relayer fee for a cross-chain transfer"],
  ica: () => ["Used an interchain account"],
  relay: () => ["Relayed cross-chain packets"],

  dex_order: () => ["Placed an order on the DEX"],
  dex_cancel: (i) => {
    const d = ownEvent(i.row, "bze.tradebin.OrderCanceledEvent", "owner", i.address);
    return [d ? `Order cancelled on ${marketName(attr(d, "market_id"), i.ctx)}` : "Cancelled an order"];
  },
  dex_fill: (i) => {
    const fills = fillPhrases(i.row, i.address, i.ctx);
    // A fill message's own row: its fills settle at the end of the block, in rows of their own.
    if (fills.length === 0) return ["Sent orders to fill on the DEX"];
    const more = fills.length - 1;
    return [`Order filled · ${fills[0]}${more > 0 ? ` + ${more} more` : ""}`];
  },
  dex_order_saved: (i) => {
    const d = ownEvent(i.row, "bze.tradebin.OrderSavedEvent", "owner", i.address);
    const m = d ? splitMarket(attr(d, "market_id")) : null;
    const amount = d ? attr(d, "amount") : null;
    if (!m || !amount) return ["Order placed on the book"];
    return [`Order placed on the book · ${coinText({ denom: m.base, amount }, i.ctx)} still waiting`];
  },
  dex_swap: (i) => {
    if (i.losses.length && i.gains.length) return [`Swapped ${amounts(i.losses, i.ctx)} for ${amounts(i.gains, i.ctx)}`];
    return ["Swapped on the DEX"];
  },
  liquidity: ({ row }) => [
    firstType(row, "MsgRemoveLiquidity") ? "Removed liquidity from a DEX pool" : "Added liquidity to a DEX pool",
  ],
  dex_pool: () => ["Created a DEX liquidity pool"],
  dex_market: () => ["Opened a DEX market"],

  burner_fund: () => ["Funded the Burner"],
  burner_move: () => ["Moved locked cross-chain coins to the Burner"],
  raffle_start: () => ["Started a raffle"],
  raffle_join: () => ["Joined a raffle"],
  raffle_won: (i) => {
    const d = ownEvent(i.row, "bze.burner.RaffleWinnerEvent", "winner", i.address);
    const denom = d ? attr(d, "denom") : null;
    const amount = d ? attr(d, "amount") : null;
    const won = i.gains.length ? amounts(i.gains, i.ctx) : denom && amount ? coinText({ denom, amount }, i.ctx) : null;
    return [won ? `Won the raffle · ${won}` : "Won the raffle"];
  },
  raffle_lost: () => ["Raffle ticket did not win"],
  raffle_finished: () => ["A raffle finished"],
  burned: () => ["The Burner burned coins"],

  token_create: () => ["Created a token"],
  token_mint: (i) => [i.gains.length ? `Minted ${amounts(i.gains, i.ctx)}` : "Minted tokens"],
  token_burn: (i) => [i.losses.length ? `Burned ${amounts(i.losses, i.ctx)}` : "Burned tokens"],
  token_admin: by(
    (i) => withParty(i, "Handed a token's admin to", "Changed a token's admin"),
    (i) => withParty(i, "Became a token's admin, from", "Became a token's admin"),
  ),
  token_update: ({ row }) => [
    firstType(row, "MsgSetDenomBranding") ? "Updated a token's branding" : "Updated a token's name, symbol and decimals",
  ],

  reward_join: (i) => [i.losses.length ? `Joined a reward program · ${amounts(i.losses, i.ctx)} locked` : "Joined a reward program"],
  reward_exit: () => ["Left a reward program"],
  reward_claim: (i) => [i.gains.length ? `Claimed reward prizes · ${amounts(i.gains, i.ctx)}` : "Claimed reward prizes"],
  reward_program: ({ row }) => {
    const words: Record<string, string> = {
      MsgCreateStakingReward: "Opened a staking reward",
      MsgUpdateStakingReward: "Extended a staking reward",
      MsgDeleteStakingReward: "Closed a staking reward",
      MsgDistributeStakingRewards: "Added prizes to a staking reward",
      MsgCreateTradingReward: "Proposed a trading reward",
      MsgActivateTradingReward: "Started a trading reward",
      MsgCreateDenomReward: "Opened a holder reward",
      MsgCreateDenomRewardSchedule: "Scheduled a holder reward",
      MsgUpdateDenomRewardSchedule: "Changed a holder reward's schedule",
      MsgDistributeDenomRewards: "Paid out a holder reward",
    };
    const name = firstType(row, ...Object.keys(words));
    return [name ? words[name] : "A reward program changed"];
  },
  reward_won: (i) => {
    const d = detailsOf(i.row, "bze.rewards.TradingRewardDistributionEvent")[0];
    const denom = d ? attr(d, "prize_denom") : null;
    const amount = d ? attr(d, "prize_amount") : null;
    const won = i.gains.length ? amounts(i.gains, i.ctx) : denom && amount ? coinText({ denom, amount }, i.ctx) : null;
    return [won ? `Won trading rewards · ${won}` : "Won trading rewards"];
  },

  article: by(
    () => ["Published an article on CoinTrunk"],
    (i) => withParty(i, "Respect received from", "Respect received"),
  ),
  respect: by(
    (i) => withParty(i, "Paid respect to", "Paid respect to a CoinTrunk publisher"),
    (i) => withParty(i, "Respect received from", "Respect received"),
  ),

  fee_only: ({ row }) => {
    const first = row.msg_types[0];
    const what = first ? humanise(first) : "A transaction";
    return [row.success ? `Paid the fee of: ${what}` : what];
  },
};

/** The kinds the renderer knows, for the walk test. */
export const KNOWN_KINDS: readonly string[] = Object.keys(SENTENCES);

/** `other` and any kind added after this renderer: the first message type in words. */
const otherSentence: Sentence = ({ row }) => [row.msg_types[0] ? humanise(row.msg_types[0]) : "Something happened"];

/**
 * A row in plain words: the sentence (with the counterparty as a chip), and
 * its chips: the cross-chain status from `ibc` (the row's transfer, see
 * `ibcFor`) and "Failed" for a failed transaction.
 */
export function activityLine(
  row: Activity,
  { address, ctx, ibc = null }: { address: string; ctx: RenderContext; ibc?: IbcTransfer | null },
): ActivityLine {
  const moved = withoutFee(row);
  const input: Input = {
    row,
    address,
    ctx,
    ibc,
    party: row.counterparty ? { party: row.counterparty } : null,
    gains: moved.filter(isPositive),
    losses: moved.filter((b) => !isPositive(b)).map(abs),
  };
  const known = Object.prototype.hasOwnProperty.call(SENTENCES, row.kind) && row.kind !== "other";
  const parts = (known ? SENTENCES[row.kind] : otherSentence)(input).filter((p) => p !== "");
  const chips: StatusChip[] = [];
  if (ibc) chips.push(ibcChip(ibc));
  if (!row.success) chips.push({ label: "Failed", palette: "red" });
  return { parts, chips, fallback: !known };
}

/** The second line's block part for a row without a transaction of the address. */
export const BLOCK_LEVEL_NOTE = "no transaction of yours";
