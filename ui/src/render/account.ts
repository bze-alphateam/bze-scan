import type { Account, AccountUnbonding, Balance, Coin, Label, LpPair, OpenOrders, TokenSummary, Validator } from "@/api/types";
import { formatCount, formatDateTime, formatPct, shortAddress } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { compareDecimals, isAboveZero, percentOf, sumAmounts, usdValue } from "@/lib/math";

import { uptimeText } from "./validators";

// The account page's words and numbers, kept pure so they are tested without
// rendering. Amounts stay in base units until formatted.

/** The bond denom: staking, rewards and the Available tile are `ubze`. */
export const BOND_DENOM = "ubze";

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const DAY_MS = 86_400_000;

export type AccountTab = "activity" | "tokens" | "orders" | "staking";

/** A personal account's tabs, in order; the first is the default. */
export const ACCOUNT_TABS: AccountTab[] = ["activity", "tokens", "orders", "staking"];

/** The tab a `?tab=` value names; the Activity tab for anything else. */
export function parseTab(value: string | string[] | undefined): AccountTab {
  return ACCOUNT_TABS.find((t) => t === value) ?? "activity";
}

/** A module account's tabs: Holdings (`?tab=tokens`, the default) and Movements. */
export type ModuleTab = "tokens" | "movements";

export function parseModuleTab(value: string | string[] | undefined): ModuleTab {
  return value === "movements" ? "movements" : "tokens";
}

/** A `?page=` value: a positive whole number, else page 1. */
export function parsePage(value: string | string[] | undefined): number {
  const v = Array.isArray(value) ? value[0] : value;
  if (v == null || !/^[1-9]\d{0,8}$/.test(v)) return 1;
  return Number(v);
}

/** The last numbered page of `total` rows, `perPage` a page; 1 for none. */
export function lastPage(total: number, perPage: number): number {
  return Math.max(1, Math.ceil(total / perPage));
}

/** "Activity 51–75 of 534"; "Activity 0 of 0" when there is none. */
export function activityRange(page: number, perPage: number, total: number): string {
  const from = (page - 1) * perPage + 1;
  if (total === 0 || from > total) return `Activity 0 of ${formatCount(total)}`;
  return `Activity ${formatCount(from)}–${formatCount(Math.min(total, page * perPage))} of ${formatCount(total)}`;
}

/**
 * The URL of the Activity tab's place: `?tab=activity&category=dex&page=3`,
 * the defaults (all categories, page 1) left out; nothing at all for the default place.
 */
export function activitySearch(category: string | null, page: number): string {
  const q = new URLSearchParams();
  if (category) {
    q.set("tab", "activity");
    q.set("category", category);
  }
  if (page > 1) {
    q.set("tab", "activity");
    q.set("page", String(page));
  }
  const s = q.toString();
  return s ? `?${s}` : "";
}

/** What the module account page says about a module: who owns it, and when its coins move. */
export interface ModuleFacts {
  /** One sentence; a `word` in backticks is a module name, shown in mono. */
  owner: string;
  moves: string;
  movesSub: string;
}

const MODULES: Record<string, ModuleFacts> = {
  tradebin: {
    owner: "Owned by the chain's DEX module (`tradebin`). Holds the coins in liquidity pools and open orders.",
    moves: "people trade",
    movesSub: "add or remove liquidity, place, fill or cancel orders",
  },
  burner: {
    owner: "Owned by the Burner module (`burner`). Collects coins to destroy at the next periodic burn.",
    moves: "coins are burned",
    movesSub: "sent to burn, and at every periodic burn",
  },
  burner_black_hole: {
    owner: "Owned by the Burner module. Holds coins locked forever.",
    moves: "coins are locked",
    movesSub: "coins only come in, never leave",
  },
  burner_raffle: {
    owner: "Owned by the Burner module. Holds the burner raffles' pots.",
    moves: "raffles run",
    movesSub: "people join, raffles pay out",
  },
  fee_collector: {
    owner: "Owned by the chain's auth module (`fee_collector`). Collects every transaction fee paid in BZE.",
    moves: "every block",
    movesSub: "fees come in, then go to stakers and the community pool",
  },
  txfeecollector: {
    owner: "Owned by BZE's fee module (`txfeecollector`). Collects fees paid in other tokens and converts them to BZE.",
    moves: "fees are paid in other tokens",
    movesSub: "converted, then sent to the fee collector",
  },
  txfeecollector_burner: {
    owner: "Owned by BZE's fee module. Collects the fees that go to the Burner.",
    moves: "fees are paid",
    movesSub: "passed on to the Burner",
  },
  txfeecollector_cp: {
    owner: "Owned by BZE's fee module. Collects the fees that go to the community pool.",
    moves: "fees are paid",
    movesSub: "passed on to the community pool",
  },
  distribution: {
    owner: "Owned by the distribution module. Holds staking rewards until claimed, and the community pool.",
    moves: "every block",
    movesSub: "rewards added and claimed, community pool spends",
  },
  mint: {
    owner: "Owned by the mint module. Creates the new BZE of each block.",
    moves: "every block",
    movesSub: "new BZE created and passed on",
  },
  bonded_tokens_pool: {
    owner: "Owned by the staking module. Holds all staked BZE.",
    moves: "people stake",
    movesSub: "stake, unstake, redelegate, slashing",
  },
  not_bonded_tokens_pool: {
    owner: "Owned by the staking module. Holds BZE during the unstaking period.",
    moves: "unstaking starts or ends",
    movesSub: "released after the unstaking period",
  },
  gov: {
    owner: "Owned by the governance module. Holds proposal deposits.",
    moves: "proposals get deposits",
    movesSub: "deposits come in, are returned or burned",
  },
  rewards: {
    owner: "Owned by the rewards module. Holds the prizes of staking and trading reward programs.",
    moves: "reward programs run",
    movesSub: "programs open, pay out, people join or leave",
  },
  tokenfactory: {
    owner: "Owned by the token factory module.",
    moves: "tokens are created",
    movesSub: "creation fees, minting and burning",
  },
  transfer: {
    owner: "Owned by the cross-chain transfer module.",
    moves: "cross-chain transfers",
    movesSub: "tokens minted or burned for transfers",
  },
  feeibc: {
    owner: "Owned by the cross-chain relayer fee module.",
    moves: "relayer fees are paid",
    movesSub: "fees escrowed for relayers",
  },
  interchainaccounts: {
    owner: "Owned by the interchain accounts module.",
    moves: "rarely",
    movesSub: "interchain account actions",
  },
  nft: {
    owner: "Owned by the NFT module.",
    moves: "rarely",
    movesSub: "not used on BZE",
  },
};

/** The facts of a module by its `module` name (from `/modules`); a plain fallback for one this list lacks, or while unknown. */
export function moduleFacts(module: string | null | undefined): ModuleFacts {
  const known = module ? MODULES[module] : undefined;
  if (known) return known;
  return {
    owner: module ? `Owned by the chain's \`${module}\` module.` : "Owned by one of the chain's modules.",
    moves: "as the module's rules say",
    movesSub: "inside other people's transactions",
  };
}

/** Where a module's BZE sits, for the BZE held tile. */
export function moduleBondWhere(module: string | null | undefined): string {
  return module === "tradebin" ? "in pools and open orders" : "held by the module";
}

/** The page's name: the label, else "Account bze1q8f…u4a". */
export function accountTitle(address: string, label: Label | null | undefined): string {
  return label?.name || `Account ${shortAddress(address)}`;
}

/** "active since Apr 2022 · 1,204 transactions", or "no transactions seen yet". */
export function activeSince(a: Pick<Account, "first_seen" | "tx_count">): string {
  if (!a.first_seen) return "no transactions seen yet";
  const d = new Date(a.first_seen.time);
  const since = `active since ${MONTHS[d.getUTCMonth()]} ${d.getUTCFullYear()}`;
  return `${since} · ${formatCount(a.tx_count)} ${a.tx_count === 1 ? "transaction" : "transactions"}`;
}

/** The amount of `denom` among the coins, "0" when absent. */
export function amountOf(coins: Coin[], denom: string): string {
  return coins.find((c) => c.denom === denom)?.amount ?? "0";
}

/** Whole days until `time`, at least 1 while it is still ahead; 0 once passed. */
export function daysUntil(time: string, now: Date): number {
  const ms = new Date(time).getTime() - now.getTime();
  return ms <= 0 ? 0 : Math.ceil(ms / DAY_MS);
}

/** "1 day", "21 days", "today". */
export function daysText(days: number): string {
  if (days <= 0) return "today";
  return `${formatCount(days)} ${days === 1 ? "day" : "days"}`;
}

/** The Unstaking tile: the total and the earliest unlock; null values when nothing unstakes. */
export function unstaking(entries: AccountUnbonding[], now: Date): { total: string; nextDays: number | null } {
  if (entries.length === 0) return { total: "0", nextDays: null };
  const earliest = entries.reduce((a, b) => (new Date(b.completion_time) < new Date(a.completion_time) ? b : a));
  return { total: sumAmounts(entries.map((e) => e.amount)), nextDays: daysUntil(earliest.completion_time, now) };
}

/** An entry of the Unstaking list: "back in your account on Oct 30, 2026, 10:00:00 UTC (19 days)". */
export function unlockText(e: AccountUnbonding, now: Date): string {
  return `back in your account on ${formatDateTime(e.completion_time)} (${daysText(daysUntil(e.completion_time, now))})`;
}

/** Where a token comes from, by its kind; the raw denom for one the explorer does not know. */
/**
 * A multi-hop IBC token: how many chains it crossed and the one it came
 * through. It is not the same token as the one sent straight from its home
 * chain, whatever their symbols say. Null for one hop and the other kinds.
 */
export function multiHop(token: Pick<TokenSummary, "ibc_hops" | "via_chain">): { hops: number; via: string | null } | null {
  if (token.ibc_hops == null || token.ibc_hops < 2) return null;
  return { hops: token.ibc_hops, via: token.via_chain?.name || token.via_chain?.chain_id || null };
}

export function originTag(token: TokenSummary | undefined, denom: string): string {
  switch (token?.kind) {
    case "native":
      return "Native";
    case "factory":
      return "Factory";
    case "ibc": {
      const chain = token.origin_chain?.name || token.origin_chain?.chain_id;
      const hop = multiHop(token);
      const via = hop?.via ? ` via ${hop.via}` : "";
      return chain ? `Cross-chain · ${chain}${via}` : "Cross-chain";
    }
    case "lp":
      return "DEX pool share";
    default:
      return denom;
  }
}

/** The token's page on the DEX. */
export function dexAssetUrl(denom: string): string {
  return `${externalLinks.dex}/assets/details?denom=${encodeURIComponent(denom)}`;
}

export interface TokenRow {
  denom: string;
  amount: string;
  /** Null for a denom the explorer does not know: the raw amount and denom are shown. */
  symbol: string | null;
  exponent: number | null;
  name: string | null;
  logoUrl: string | null;
  /** A pool share's two assets, for its paired logo. */
  lpPair: LpPair | null;
  origin: string;
  /** The token as the tokens list knows it; null for a denom the explorer does not know. */
  token: TokenSummary | null;
  /** The balance in USD; null without a price. */
  valueUsd: string | null;
}

export type TokenSort = "value" | "name";

const rowName = (r: TokenRow) => (r.symbol ?? r.denom).toLowerCase();

/**
 * The Tokens tab: every non-zero balance joined to the tokens list, sorted by
 * value (priced first, largest first, then by name) or by name.
 */
export function tokenRows(balances: Balance[], denoms: Map<string, TokenSummary> | undefined, sort: TokenSort): TokenRow[] {
  const rows = balances
    .filter((b) => isAboveZero(b.amount))
    .map((b): TokenRow => {
      const t = denoms?.get(b.denom);
      const symbol = t?.symbol ?? b.symbol;
      const exponent = t?.exponent ?? b.exponent;
      return {
        denom: b.denom,
        amount: b.amount,
        symbol,
        exponent,
        name: t?.name ?? null,
        logoUrl: t?.logo_url ?? null,
        lpPair: t?.lp_pair ?? null,
        origin: originTag(t, b.denom),
        token: t ?? null,
        valueUsd: exponent != null ? usdValue(b.amount, exponent, t?.price_usd) : null,
      };
    });
  const byName = (a: TokenRow, b: TokenRow) => rowName(a).localeCompare(rowName(b));
  if (sort === "name") return rows.sort(byName);
  return rows.sort((a, b) => {
    if (a.valueUsd != null && b.valueUsd != null) return compareDecimals(b.valueUsd, a.valueUsd) || byName(a, b);
    if (a.valueUsd != null) return -1;
    if (b.valueUsd != null) return 1;
    return byName(a, b);
  });
}

/** The priced rows' total in USD. */
export function tokensTotalUsd(rows: TokenRow[]): string {
  return sumAmounts(rows.flatMap((r) => (r.valueUsd != null ? [r.valueUsd] : [])));
}

/** "1 order", "6 orders". */
export function ordersText(n: number): string {
  return `${formatCount(n)} ${n === 1 ? "order" : "orders"}`;
}

/** "1 market", "3 markets". */
export function marketsText(n: number): string {
  return `${formatCount(n)} ${n === 1 ? "market" : "markets"}`;
}

/** One market of an Orders tab row: what the account's orders there lock of the row's asset. */
export interface OrderMarketRow {
  marketId: string;
  amount: string;
  ordersCount: number;
  /** In USD; null without a price. */
  valueUsd: string | null;
}

/** One asset of the Orders tab: the amount locked in open orders, then per market. */
export interface OrderAssetRow {
  denom: string;
  amount: string;
  symbol: string | null;
  exponent: number | null;
  logoUrl: string | null;
  lpPair: LpPair | null;
  /** The token as the tokens list knows it; null for a denom the explorer does not know. */
  token: TokenSummary | null;
  valueUsd: string | null;
  ordersCount: number;
  markets: OrderMarketRow[];
}

const valueOf = (amount: string, t: TokenSummary | undefined) =>
  t?.exponent != null ? usdValue(amount, t.exponent, t.price_usd) : null;

/**
 * The Orders tab's rows: every asset the open orders lock, with its value at
 * DEX prices, sorted by value (highest first), then the unpriced ones by
 * name. Markets keep the API's order (largest amount first).
 */
export function orderRows(orders: OpenOrders, denoms: Map<string, TokenSummary> | undefined): OrderAssetRow[] {
  const rows = orders.assets.map((a): OrderAssetRow => {
    const t = denoms?.get(a.denom);
    return {
      denom: a.denom,
      amount: a.amount,
      symbol: t?.symbol ?? null,
      exponent: t?.exponent ?? null,
      logoUrl: t?.logo_url ?? null,
      lpPair: t?.lp_pair ?? null,
      token: t ?? null,
      valueUsd: valueOf(a.amount, t),
      ordersCount: a.orders_count,
      markets: a.markets.map((m) => ({
        marketId: m.market_id,
        amount: m.amount,
        ordersCount: m.orders_count,
        valueUsd: valueOf(m.amount, t),
      })),
    };
  });
  const name = (r: OrderAssetRow) => (r.symbol ?? r.denom).toLowerCase();
  const byName = (a: OrderAssetRow, b: OrderAssetRow) => name(a).localeCompare(name(b));
  return rows.sort((a, b) => {
    if (a.valueUsd != null && b.valueUsd != null) return compareDecimals(b.valueUsd, a.valueUsd) || byName(a, b);
    if (a.valueUsd != null) return -1;
    if (b.valueUsd != null) return 1;
    return byName(a, b);
  });
}

/** The priced assets' total in USD, and whether some asset has no price (the head line then says so). */
export function ordersTotalUsd(rows: OrderAssetRow[]): { total: string; partial: boolean } {
  return {
    total: sumAmounts(rows.flatMap((r) => (r.valueUsd != null ? [r.valueUsd] : []))),
    partial: rows.some((r) => r.valueUsd == null),
  };
}

/** The amount each denom has locked in open orders, for the Tokens tab's "in orders" lines. */
export function lockedByDenom(orders: OpenOrders | undefined): Map<string, string> {
  return new Map((orders?.assets ?? []).filter((a) => isAboveZero(a.amount)).map((a) => [a.denom, a.amount]));
}

export interface StakingRow {
  validator: string;
  moniker: string | null;
  amount: string;
  /** "41.7%" of the account's stake. */
  share: string;
  /** Pending `ubze` rewards from this validator; null when none. */
  rewards: string | null;
}

/** The Staking tab's rows, in the API's order. */
export function stakingRows(a: Pick<Account, "delegations" | "rewards" | "total_staked">): StakingRow[] {
  return a.delegations.map((d) => {
    const share = percentOf(d.amount, a.total_staked);
    const reward = a.rewards.find((r) => r.validator === d.validator);
    const amount = reward ? amountOf(reward.coins, BOND_DENOM) : "0";
    return {
      validator: d.validator,
      moniker: d.moniker,
      amount: d.amount,
      share: share != null ? formatPct(share, { trim: true }) : "—",
      rewards: isAboveZero(amount) ? amount : null,
    };
  });
}

/** A validator's status chip on the Staking tab, and its uptime when below 99%. */
export function validatorStatus(v: Validator | undefined): { text: string; tone: "green" | "gray" | "red"; uptime: string | null } | null {
  if (!v) return null;
  const uptime = v.uptime != null && compareDecimals(v.uptime, "99") < 0 ? `${uptimeText(v.uptime)} uptime` : null;
  if (v.jailed) return { text: "Jailed", tone: "red", uptime };
  if (v.status === "bonded") return { text: "Active", tone: "green", uptime };
  return { text: "Inactive", tone: "gray", uptime };
}
