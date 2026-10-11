import type { Account, AccountUnbonding, Balance, Coin, Label, TokenSummary, Validator } from "@/api/types";
import { formatCount, formatDateTime, formatPct, shortAddress } from "@/lib/format";
import { compareDecimals, isAboveZero, percentOf, sumAmounts, usdValue } from "@/lib/math";

import { uptimeText } from "./validators";

// The account page's words and numbers, kept pure so they are tested without
// rendering. Amounts stay in base units until formatted.

/** The bond denom: staking, rewards and the Available tile are `ubze`. */
export const BOND_DENOM = "ubze";

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const DAY_MS = 86_400_000;

export type AccountTab = "activity" | "tokens" | "staking";

/** The tabs, in order; the first is the default. */
export const ACCOUNT_TABS: AccountTab[] = ["activity", "tokens", "staking"];

/** The tab a `?tab=` value names; the Activity tab for anything else. */
export function parseTab(value: string | string[] | undefined): AccountTab {
  return ACCOUNT_TABS.find((t) => t === value) ?? "activity";
}

/** The page's name: the label, else "Account bze1q8f…u4a". */
export function accountTitle(address: string, label: Label | null | undefined): string {
  return label?.name || `Account ${shortAddress(address)}`;
}

/** The chip next to a labelled account's name; null for a plain account. */
export function labelChip(label: Label | null | undefined): { text: string; dashed: boolean } | null {
  if (!label) return null;
  switch (label.kind) {
    case "module":
      return { text: "module account", dashed: true };
    case "validator_owner":
      return { text: `owner of ${label.name}`, dashed: false };
    case "ibc_escrow":
      return { text: "cross-chain escrow", dashed: false };
    default:
      return { text: label.name, dashed: false };
  }
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
export function originTag(token: TokenSummary | undefined, denom: string): string {
  switch (token?.kind) {
    case "native":
      return "Native";
    case "factory":
      return "Factory";
    case "ibc": {
      const chain = token.origin_chain?.name || token.origin_chain?.chain_id;
      return chain ? `Cross-chain · ${chain}` : "Cross-chain";
    }
    case "lp":
      return "DEX pool share";
    default:
      return denom;
  }
}

/** The token's page on the DEX. */
export function dexAssetUrl(denom: string): string {
  return `https://dex.getbze.com/assets/details?denom=${encodeURIComponent(denom)}`;
}

export interface TokenRow {
  denom: string;
  amount: string;
  /** Null for a denom the explorer does not know: the raw amount and denom are shown. */
  symbol: string | null;
  exponent: number | null;
  name: string | null;
  logoUrl: string | null;
  origin: string;
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
        origin: originTag(t, b.denom),
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
