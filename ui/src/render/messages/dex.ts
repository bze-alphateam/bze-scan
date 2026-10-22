import BigNumber from "bignumber.js";

import type { Coin, FailedOrderMessage, Fill, Order, Refund, SkippedCancel } from "@/api/types";
import { externalLinks } from "@/lib/links";

import { eventAttr, str } from "./common";
import type { RenderContext, RenderMessage } from "./types";

// The order book's plain words: market ids, prices in display units, and the
// part of the transaction's `dex` outcome that belongs to one message.

/** How many `/`-separated parts a denom starting with `head` has: `ibc/HASH`, `factory/creator/sub`. */
const DENOM_PARTS: Record<string, number> = { ibc: 2, factory: 3 };

function denomLength(parts: string[]): number {
  return DENOM_PARTS[parts[0]] ?? 1;
}

/**
 * A market id `base/quote` split into its denoms: either can hold `/` (an IBC
 * voucher, a factory token), so the base takes as many parts as its kind has.
 * Null when it does not split into two denoms.
 */
export function splitMarket(marketId: string | null): { base: string; quote: string } | null {
  if (!marketId) return null;
  const parts = marketId.split("/");
  const n = denomLength(parts);
  const rest = parts.slice(n);
  if (parts.length < n || rest.length === 0 || rest.length !== denomLength(rest) || parts.includes("")) return null;
  return { base: parts.slice(0, n).join("/"), quote: rest.join("/") };
}

/** A denom's symbol, else the denom itself. */
export function symbolOf(denom: string, ctx: RenderContext): string {
  return ctx.denom(denom).symbol ?? denom;
}

/** `BZE/USDC`; the raw market id when a denom is unknown. */
export function marketName(marketId: string | null, ctx: RenderContext): string {
  const m = splitMarket(marketId);
  if (!m) return marketId ?? "an unknown market";
  const base = ctx.denom(m.base).symbol;
  const quote = ctx.denom(m.quote).symbol;
  return base != null && quote != null ? `${base}/${quote}` : (marketId as string);
}

/** The market's page on the DEX app. */
export function dexMarketUrl(marketId: string): string {
  return `${externalLinks.dex}/exchange/market?id=${encodeURIComponent(marketId)}`;
}

/**
 * A price as the chain stores it (quote base units per base base unit) in
 * display units: `0.0125 USDC`; the raw price and quote denom when either
 * exponent is unknown.
 */
export function priceText(price: string | null, marketId: string | null, ctx: RenderContext): string {
  const m = splitMarket(marketId);
  if (price == null) return "an unknown price";
  if (!m) return price;
  const base = ctx.denom(m.base);
  const quote = ctx.denom(m.quote);
  if (base.exponent == null || quote.exponent == null || quote.symbol == null) return `${price} ${m.quote}`;
  const display = new BigNumber(price).shiftedBy(base.exponent - quote.exponent);
  if (!display.isFinite()) return `${price} ${m.quote}`;
  return `${display.decimalPlaces(18, BigNumber.ROUND_DOWN).toFormat({ decimalSeparator: ".", groupSeparator: ",", groupSize: 3 })} ${quote.symbol}`;
}

/** The quote amount of `amount` base units at `price`, in quote base units (rounded down, as the chain does). */
export function quoteAmount(amount: string | null, price: string | null): string | null {
  if (amount == null || price == null) return null;
  const total = new BigNumber(amount).times(price);
  return total.isFinite() ? total.integerValue(BigNumber.ROUND_DOWN).toFixed() : null;
}

/** "buy" or "sell"; null for anything else. */
export function orderSide(value: string | null): "buy" | "sell" | null {
  return value === "buy" || value === "sell" ? value : null;
}

/** A base and a quote coin of a market. */
export function marketCoins(
  marketId: string | null,
  amount: string | null,
  price: string | null,
): { base: Coin; quote: Coin | null } | null {
  const m = splitMarket(marketId);
  if (!m || amount == null) return null;
  const q = quoteAmount(amount, price);
  return {
    base: { denom: m.base, amount },
    quote: q == null ? null : { denom: m.quote, amount: q },
  };
}

/** What `dex` says about one message. */
export interface MessageOutcome {
  placed: Order[];
  fills: Fill[];
  cancelled: Order[];
  /** What the order book sent back (chain v8.3.0). */
  refunds: Refund[];
  /** Cancels that found no order (chain v8.3.0). */
  skippedCancels: SkippedCancel[];
  /** Messages the order book failed to process and retries (chain v8.3.0). */
  failed: FailedOrderMessage[];
  /** The order book has not processed this transaction's messages yet. */
  pending: boolean;
}

const CREATE_EVENT = "bze.tradebin.OrderCreateMessageEvent";
const CANCEL_EVENT = "bze.tradebin.OrderCancelMessageEvent";

/** The queue message ids a message's events name (chain v8.2.0 and later). */
function messageIds(msg: RenderMessage): Set<string> {
  const ids = new Set<string>();
  for (const e of msg.events ?? []) {
    if (e.type !== CREATE_EVENT && e.type !== CANCEL_EVENT) continue;
    const id = e.attrs.message_id;
    if (typeof id === "string" && id !== "") ids.add(id);
  }
  return ids;
}

/**
 * The part of the transaction's DEX outcome that belongs to one message:
 * orders and fills whose queue message id one of its events names; before
 * chain v8.2.0 (no ids) those of its market and owner. A cancel's order is
 * the one its body names. Null when the transaction has no outcome.
 */
export function outcomeOf(msg: RenderMessage, ctx: RenderContext): MessageOutcome | null {
  const dex = ctx.dex;
  if (!dex) return null;
  const ids = messageIds(msg);
  const market = str(msg, "market_id");
  const creator = str(msg, "creator");
  const own = (messageId: string | null, marketId: string, owner: string) =>
    ids.size > 0 ? messageId != null && ids.has(messageId) : marketId === market && owner === creator;
  const orderId = str(msg, "order_id") ?? eventAttr(msg, CANCEL_EVENT, "orderId");
  return {
    placed: dex.placed.filter((o) => own(o.message_id, o.market_id, o.owner)),
    fills: dex.fills.filter((f) => own(f.taker_message_id, f.market_id, f.taker)),
    cancelled: orderId == null ? [] : dex.cancelled.filter((o) => o.order_id === orderId && o.market_id === market),
    // These carry a message id always: they exist only since chain v8.3.0.
    refunds: (dex.refunds ?? []).filter((r) => ids.has(r.message_id)),
    skippedCancels: (dex.skipped_cancels ?? []).filter((c) => ids.has(c.message_id)),
    failed: (dex.failed ?? []).filter((f) => ids.has(f.message_id)),
    pending: dex.pending,
  };
}

/** The sum of the fills' base amounts and their quote amounts, base units. */
export function fillTotals(fills: Fill[]): { base: string; quote: string } {
  let base = new BigNumber(0);
  let quote = new BigNumber(0);
  for (const f of fills) {
    base = base.plus(f.amount);
    quote = quote.plus(new BigNumber(f.amount).times(f.price).integerValue(BigNumber.ROUND_DOWN));
  }
  return { base: base.toFixed(), quote: quote.toFixed() };
}

/**
 * What refunds sent back, one coin per denom (a market's messages refund one
 * denom), and whether one of them was only dust.
 */
export function refundTotals(refunds: Refund[]): { coins: Coin[]; dust: boolean; base: string } {
  const byDenom = new Map<string, BigNumber>();
  let dust = false;
  let base = new BigNumber(0);
  for (const r of refunds) {
    base = base.plus(r.amount);
    if (!r.refund || !new BigNumber(r.refund.amount).gt(0)) {
      dust = true;
      continue;
    }
    byDenom.set(r.refund.denom, (byDenom.get(r.refund.denom) ?? new BigNumber(0)).plus(r.refund.amount));
  }
  return { coins: [...byDenom].map(([denom, amount]) => ({ denom, amount: amount.toFixed() })), dust, base: base.toFixed() };
}

/** How much of an order is filled, a whole percentage rounded down (0–100). */
export function filledPct(amount: string, filled: string): number {
  const a = new BigNumber(amount);
  if (!a.gt(0)) return 0;
  return Math.min(100, new BigNumber(filled).div(a).times(100).integerValue(BigNumber.ROUND_DOWN).toNumber());
}

/** What a cancelled order gave back: the unfilled base (sell) or its quote at the order price (buy). */
export function returnedCoin(order: Order): Coin | null {
  const m = splitMarket(order.market_id);
  if (!m) return null;
  const rest = BigNumber.max(new BigNumber(order.amount).minus(order.filled_amount), 0).toFixed();
  if (order.side === "sell") return { denom: m.base, amount: rest };
  const q = quoteAmount(rest, order.price);
  return q == null ? null : { denom: m.quote, amount: q };
}
