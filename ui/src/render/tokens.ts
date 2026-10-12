import type { Label, OriginChain, Token, TokenEvent, TokenHolder, TokenSummary, TokenTransfer } from "@/api/types";
import { formatAmount, formatCount, formatPct, formatUsd } from "@/lib/format";
import { compareDecimals, sumAmounts } from "@/lib/math";

import { multiHop } from "./account";

// The tokens pages' words and numbers, kept pure so they are tested without
// rendering. Amounts stay in base units until formatted.

/** The DEX (tradebin) module account: a transfer to or from it is a trade. */
export const DEX_MODULE_ADDRESS = "bze18mhtjwczlzqqgvw84uz8lrdv4hqule3jp8allp";

/** The explorer page of a denom: the base denom URL-encoded as one segment. */
export function tokenHref(denom: string): string {
  return `/tokens/${encodeURIComponent(denom)}`;
}

/**
 * The denom of a `/tokens/...` URL: one encoded segment (`factory%2Fbze1…%2Fuhoney`)
 * or several plain ones (`factory/bze1…/uhoney`), each decoded once. Null for
 * a segment that does not decode.
 */
export function denomFromSegments(segments: string | string[]): string | null {
  try {
    return (Array.isArray(segments) ? segments : [segments]).map((s) => decodeURIComponent(s)).join("/");
  } catch {
    return null;
  }
}

/** The name a token goes by: its symbol, else its denom. */
export const tokenSymbol = (t: Pick<TokenSummary, "symbol" | "denom">) => t.symbol ?? t.denom;

/** "2 hops via Osmosis": the chains a multi-hop token crossed to reach BZE. */
export function hopsText(hop: { hops: number; via: string | null }): string {
  return `${hop.hops} hops${hop.via ? ` via ${hop.via}` : ""}`;
}

/**
 * Why a multi-hop token is a token of its own: it reached BZE through another
 * chain, so it is a different denom from the same asset sent straight from
 * home. Null for a one-hop token and the other kinds.
 */
export function hopsExplainer(t: Pick<TokenSummary, "ibc_hops" | "via_chain" | "origin_chain" | "symbol" | "denom">): string | null {
  const hop = multiHop(t);
  if (!hop) return null;
  const home = t.origin_chain?.name || t.origin_chain?.chain_id || "its home chain";
  const via = hop.via ?? "another chain";
  return (
    `This ${tokenSymbol(t)} came to BZE through ${via} (${hop.hops} hops), not straight from ${home}. ` +
    `It is a different token from the ${tokenSymbol(t)} sent directly from ${home}: the two are not interchangeable on BZE. ` +
    `To swap one for the other, send it back the way it came.`
  );
}

export interface RouteChain {
  name: string;
  logoUrl: string | null;
}

export interface RouteHop {
  /** port/channel, as the path writes it. */
  channel: string;
  /** The chain the channel is on, and the one it leads to. */
  on: string;
  toward: string;
}

/**
 * The way an IBC token came to BZE: the chains from its home to BZE, and each
 * hop of its path. A multi-hop path names the chain of its first hop (`via`)
 * and the home chain; a chain between them is "another chain" (the explorer
 * knows only BZE's channels). Null for the other kinds and an unknown path.
 */
export function ibcRoute(
  t: Pick<TokenSummary, "kind" | "via_chain" | "origin_chain"> & { ibc_path: string | null; origin_chain_id: string | null },
): { chains: RouteChain[]; hops: RouteHop[] } | null {
  if (t.kind !== "ibc" || !t.ibc_path) return null;
  const parts = t.ibc_path.split("/");
  if (parts.length < 2 || parts.length % 2 !== 0) return null;
  const pairs: string[] = [];
  for (let i = 0; i < parts.length; i += 2) pairs.push(`${parts[i]}/${parts[i + 1]}`);
  const named = (c: OriginChain | null, fallback: string): RouteChain => ({
    name: c?.name || c?.chain_id || fallback,
    logoUrl: c?.logo_url ?? null,
  });
  const home = named(t.origin_chain, t.origin_chain_id || "its home chain");
  // From BZE outwards: BZE, the first hop's chain, …, home.
  const outward: RouteChain[] = [{ name: "BZE", logoUrl: null }];
  for (let i = 1; i < pairs.length; i++) {
    outward.push(i === 1 ? named(t.via_chain, "another chain") : { name: "another chain", logoUrl: null });
  }
  outward.push(home);
  const hops = pairs.map((channel, i) => ({ channel, on: outward[i].name, toward: outward[i + 1].name }));
  return { chains: [...outward].reverse(), hops };
}

/**
 * Denoms the BZE apps no longer list (the dapps' `EXCLUDED_ASSETS`, kept in
 * step by hand): superseded duplicates and retired tokens. The explorer keeps
 * showing them, marked obsolete.
 */
export const OBSOLETE_DENOMS: ReadonlySet<string> = new Set([
  "factory/bze1972aqfzdg29ugjln74edx0xvcg4ehvysjptk77/1000000000", // the first CTL, replaced by …/CTL
  "ibc/689DD6F80E4DBCE14877462B182504037FAEAD0699D5804A7F5CB328D33ED24B", // VDL over IBC, replaced by the Factory VDL
  "factory/bze1f0qgels0eu96ev6a67znu70q7rquy9eragn8nw/ucorey",
]);

export const OBSOLETE_EXPLAINER =
  "The BZE apps no longer list this token: it was replaced or retired. It still exists on chain, so its history and balances stay here.";

export const isObsolete = (denom: string) => OBSOLETE_DENOMS.has(denom);

export type TokenFilter = "all" | "native" | "factory" | "ibc";

export const TOKEN_FILTERS: { value: TokenFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "native", label: "Native" },
  { value: "factory", label: "Created on Factory" },
  { value: "ibc", label: "Cross-chain" },
];

/** The list's rows for a chip and the "Find a token" text (symbol or name, any case). */
export function filterTokens(tokens: TokenSummary[], filter: TokenFilter, query: string): TokenSummary[] {
  const q = query.trim().toLowerCase();
  return tokens.filter(
    (t) =>
      (filter === "all" || t.kind === filter) &&
      (q === "" || [t.symbol, t.name].some((v) => v != null && v.toLowerCase().includes(q))),
  );
}

const plural = (n: number, one: string, many: string) => `${formatCount(n)} ${n === 1 ? one : many}`;

export interface TokensTiles {
  count: number;
  /** "1 native · 6 Factory · 19 cross-chain". */
  byKind: string;
  onDex: number;
  createdThisMonth: number;
  /** The distinct origin chains of the cross-chain tokens, by name, else chain id. */
  chains: string[];
}

/** The tokens page's four tiles, counted from the whole list. */
export function tokensTiles(tokens: TokenSummary[], now: Date): TokensTiles {
  const count = (kind: string) => tokens.filter((t) => t.kind === kind).length;
  const parts = [
    `${formatCount(count("native"))} native`,
    `${formatCount(count("factory"))} Factory`,
    `${formatCount(count("ibc"))} cross-chain`,
  ];
  if (count("lp") > 0) parts.push(plural(count("lp"), "pool share", "pool shares"));
  const month = (d: Date) => d.getUTCFullYear() * 12 + d.getUTCMonth();
  const chains = new Set<string>();
  for (const t of tokens) {
    const chain = t.kind === "ibc" ? t.origin_chain?.name || t.origin_chain?.chain_id : null;
    if (chain) chains.add(chain);
  }
  return {
    count: tokens.length,
    byKind: parts.join(" · "),
    onDex: tokens.filter((t) => t.price_usd != null).length,
    createdThisMonth: tokens.filter(
      (t) => t.kind === "factory" && t.created_time != null && month(new Date(t.created_time)) === month(now),
    ).length,
    chains: [...chains].sort((a, b) => a.localeCompare(b)),
  };
}

/** A USD price without the "≈": `$1.00`, `$0.0015`. */
export function priceText(price: string): string {
  return formatUsd(price).replace(/^≈ /, "");
}

/** The 24-hour change, signed, and its tone; null without one. */
export function changeText(pct: string | null): { text: string; tone: "green" | "red" | "gray" } | null {
  if (pct == null) return null;
  const sign = compareDecimals(pct, "0");
  return { text: `${sign > 0 ? "+" : ""}${formatPct(pct)}`, tone: sign > 0 ? "green" : sign < 0 ? "red" : "gray" };
}

/** The Price · 24h cell when there is no price to show; null when there is one. */
export function noPriceText(t: Pick<TokenSummary, "halted" | "price_usd">): string | null {
  if (t.halted) return "— Halted on the DEX";
  if (t.price_usd == null) return "— no market";
  return null;
}

/** A token's supply, compact with its symbol: `500K HONEY`; null when unknown. */
export function supplyText(t: Pick<TokenSummary, "supply" | "exponent" | "symbol" | "denom">, compact = true): string | null {
  return t.supply == null ? null : formatAmount(t.supply, t.exponent, tokenSymbol(t), { compact });
}

// The token page

/**
 * Whether the supply can change, and who decides it:
 * `admin` (orange: a factory token's admin can mint and burn), `fixed` (a
 * factory token whose admin renounced), `elsewhere` (a cross-chain token,
 * minted on its own chain), `inflation` (BZE, minted every block for the
 * stakers) and `pool` (pool shares follow the pool's liquidity).
 */
export type SupplyState = "admin" | "fixed" | "elsewhere" | "inflation" | "pool";

export function supplyState(t: Pick<Token, "kind" | "admin">): SupplyState | null {
  switch (t.kind) {
    case "factory":
      return t.admin ? "admin" : "fixed";
    case "ibc":
      return "elsewhere";
    case "native":
      return "inflation";
    case "lp":
      return "pool";
    default:
      return null;
  }
}

/** The Total supply tile's sub-line. */
export function supplyTileSub(state: SupplyState | null): string | null {
  switch (state) {
    case "admin":
      return "can change · see below";
    case "fixed":
      return "fixed";
    case "elsewhere":
      return "minted on its own chain";
    case "inflation":
      return "grows with inflation";
    case "pool":
      return "follows the pool's liquidity";
    default:
      return null;
  }
}

/** "So far: 2 mints adding 1,500 HONEY, 1 burn removing 200 HONEY", from every minted and burned event. */
export function mintBurnText(events: TokenEvent[], t: Pick<Token, "exponent" | "symbol" | "denom">): string {
  const of = (kind: string) => events.filter((e) => e.kind === kind);
  const total = (list: TokenEvent[]) => formatAmount(sumAmounts(list.map((e) => e.amount ?? "0")), t.exponent, tokenSymbol(t));
  const mints = of("minted");
  const burns = of("burned");
  if (mints.length === 0 && burns.length === 0) return "So far it has not minted or burned any.";
  const parts = [];
  if (mints.length > 0) parts.push(`${plural(mints.length, "mint", "mints")} adding ${total(mints)}`);
  if (burns.length > 0) parts.push(`${plural(burns.length, "burn", "burns")} removing ${total(burns)}`);
  return `So far: ${parts.join(", ")}.`;
}

/** The About card's admin line next to the admin's chip; "renounced" when there is none. */
export function adminNote(t: Pick<Token, "admin" | "creator">): string {
  if (!t.admin) return "renounced: nobody can mint or burn";
  return t.admin === t.creator
    ? "same account · can mint, burn and change the branding"
    : "can mint, burn and change the branding";
}

/** "1 HONEY = 1,000,000 uhoney": the display unit in base units. */
export function baseUnitText(t: Pick<Token, "kind" | "denom" | "symbol" | "exponent" | "ibc_base_denom">): string {
  const base = t.kind === "factory" ? t.denom.split("/").pop()! : t.kind === "ibc" ? t.ibc_base_denom ?? t.denom : t.denom;
  return `1 ${tokenSymbol(t)} = ${formatCount(`1${"0".repeat(t.exponent)}`)} ${base}`;
}

/** The top holders' share of the supply: "the top 5 hold 62.3%"; null without shares. */
export function topHoldersText(holders: TokenHolder[]): string | null {
  const top = holders.slice(0, 5).filter((h) => h.share_pct != null);
  if (top.length === 0) return null;
  return `the top ${top.length} hold ${formatPct(sumAmounts(top.map((h) => h.share_pct!)))}`;
}

/** A party of an activity line: an address shown as a chip. */
export interface Party {
  address: string;
  label: Label | null;
}

/** An activity line: the words, then a chip when the line names a party ("Created by <chip>"). */
export interface EventLine {
  text: string;
  party: Party | null;
}

const detail = (e: TokenEvent, key: string) => {
  const v = e.details?.[key];
  return typeof v === "string" ? v : null;
};

/** A token event in words: "Minted 1,000,000 HONEY by the admin", "Admin changed to <chip>". */
export function eventLine(e: TokenEvent, t: Pick<Token, "admin" | "exponent" | "symbol" | "denom">): EventLine {
  const actor: Party | null = e.actor ? { address: e.actor, label: e.actor_label } : null;
  const amount = e.amount != null ? formatAmount(e.amount, t.exponent, tokenSymbol(t)) : "";
  const by = (text: string): EventLine =>
    actor && actor.address === t.admin ? { text: `${text} by the admin`, party: null } : { text: actor ? `${text} by` : text, party: actor };
  switch (e.kind) {
    case "created":
      return { text: actor ? "Created by" : "Created", party: actor };
    case "minted":
      return by(`Minted ${amount}`);
    case "burned":
      return by(`Burned ${amount}`);
    case "admin_changed": {
      const to = detail(e, "to");
      return to ? { text: "Admin changed to", party: { address: to, label: null } } : { text: "Admin renounced: the supply is now fixed", party: null };
    }
    case "metadata_changed":
      return by("Name and symbol updated");
    case "branding_changed":
      return by("Branding updated");
    case "market_created":
      return { text: "Market created on the DEX", party: null };
    case "pool_created":
      return { text: "Liquidity pool created on the DEX", party: null };
    case "halted":
      return { text: "Halted on the DEX by governance", party: null };
    case "unhalted":
      return { text: "Trading resumed on the DEX", party: null };
    default:
      return { text: e.kind.replace(/_/g, " "), party: null };
  }
}

/** A transfer row in words: a trade when one side is the DEX, a mint, a burn, or a send between two accounts. */
export type TransferLine =
  | { kind: "traded"; amount: string; party: Party | null }
  | { kind: "minted"; amount: string; party: Party | null }
  | { kind: "burned"; amount: string; party: Party | null }
  | { kind: "sent"; amount: string; from: Party | null; to: Party | null };

export function transferLine(x: TokenTransfer, t: Pick<Token, "exponent" | "symbol" | "denom">): TransferLine {
  const amount = formatAmount(x.amount, x.exponent ?? t.exponent, x.symbol ?? tokenSymbol(t));
  const from: Party | null = x.sender ? { address: x.sender, label: x.sender_label } : null;
  const to: Party | null = x.recipient ? { address: x.recipient, label: x.recipient_label } : null;
  if (x.kind === "mint") return { kind: "minted", amount, party: to };
  if (x.kind === "burn") return { kind: "burned", amount, party: from };
  if (x.sender === DEX_MODULE_ADDRESS) return { kind: "traded", amount, party: to };
  if (x.recipient === DEX_MODULE_ADDRESS) return { kind: "traded", amount, party: from };
  return { kind: "sent", amount, from, to };
}

export type ActivityItem =
  | { type: "event"; key: string; time: string; txHash: string | null; event: TokenEvent }
  | { type: "transfer"; key: string; time: string; txHash: string | null; transfer: TokenTransfer };

/** Orders two positions newest first: height, then transaction index. */
const newerFirst = (a: { height: number; tx_index: number }, b: { height: number; tx_index: number }) =>
  b.height - a.height || b.tx_index - a.tx_index;

/**
 * The Recent activity list: the token events merged with the loaded
 * transfers, newest first (an event before the transfers of its
 * transaction). A transaction that minted or burned is told by its event
 * alone. While older transfers remain, events older than the last loaded
 * transfer wait for them, so the list never skips back in time.
 */
export function activityItems(events: TokenEvent[], transfers: TokenTransfer[], moreTransfers: boolean): ActivityItem[] {
  const told = new Set(
    events.filter((e) => e.tx_hash && ["created", "minted", "burned"].includes(e.kind)).map((e) => e.tx_hash!.toUpperCase()),
  );
  const oldest = transfers.length > 0 ? transfers[transfers.length - 1] : null;
  const shownEvents = moreTransfers && oldest ? events.filter((e) => newerFirst(e, oldest) <= 0) : events;
  const items: ActivityItem[] = [
    ...shownEvents.map(
      (e): ActivityItem => ({ type: "event", key: `e-${e.height}-${e.tx_index}-${e.seq}`, time: e.time, txHash: e.tx_hash, event: e }),
    ),
    ...transfers
      .filter((x) => !x.tx_hash || !told.has(x.tx_hash.toUpperCase()))
      .map(
        (x): ActivityItem => ({
          type: "transfer",
          key: `t-${x.height}-${x.tx_index}-${x.seq}`,
          time: x.time,
          txHash: x.tx_hash,
          transfer: x,
        }),
      ),
  ];
  const row = (i: ActivityItem) => (i.type === "event" ? i.event : i.transfer);
  const rank = (i: ActivityItem) => (i.type === "event" ? 0 : 1);
  return items.sort((a, b) => newerFirst(row(a), row(b)) || rank(a) - rank(b) || row(a).seq - row(b).seq);
}

/** Every event of a token once: the page's newest ones and the older pages, without duplicates. */
export function mergeEvents(newest: TokenEvent[], older: TokenEvent[] | undefined): TokenEvent[] {
  const seen = new Set<string>();
  const out: TokenEvent[] = [];
  for (const e of [...newest, ...(older ?? [])]) {
    const key = `${e.height}-${e.tx_index}-${e.seq}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(e);
  }
  return out.sort((a, b) => newerFirst(a, b) || b.seq - a.seq);
}

/** Holders of at least one display unit: "accounts with at least 1 HONEY". */
export const holdersSub = (t: Pick<TokenSummary, "symbol" | "denom">) => `accounts with at least 1 ${tokenSymbol(t)}`;

/** True when the token has a market or a price on the DEX: its "View on the DEX" link is worth showing. */
export const onDex = (t: Pick<Token, "markets" | "price_usd">) => t.markets.length > 0 || t.price_usd != null;
