import BigNumber from "bignumber.js";

import type { RateLimit, RateLimitDirection } from "@/api/types";
import { formatAmount, formatPct } from "@/lib/format";

export const LIMITS_EXPLAINER =
  "Limits count the net flow (out minus in) of a 24-hour window, as a share of the token's supply on BeeZee at the window start. " +
  "Over the limit, an outgoing transfer fails and an incoming one is refunded by the sending chain.";

/** Bars turn orange from 75% used and red from 90%. */
export type BarTone = "ok" | "warn" | "bad";

export function barTone(pct: number): BarTone {
  if (pct >= 90) return "bad";
  if (pct >= 75) return "warn";
  return "ok";
}

/** What one direction of a limit says: a plain sentence, or a bar with its figures. */
export type DirectionView =
  | { kind: "blocked"; label: string; text: string }
  | { kind: "unlimited"; label: string; text: string }
  | { kind: "unenforced"; label: string; text: string }
  | { kind: "bar"; label: string; text: string; pct: number; tone: BarTone; note: string | null };

const isZero = (v: string) => new BigNumber(v).isZero();
const isFull = (v: string) => new BigNumber(v).gte(100);

/** The display symbol: the denom's symbol, else its last path segment. */
export function limitSymbol(l: Pick<RateLimit, "symbol" | "denom">): string {
  return l.symbol ?? l.denom.split("/").pop() ?? l.denom;
}

/**
 * One direction of a limit in words. `send` is outgoing from BZE, `recv`
 * incoming. A 0% quota refuses the direction, a 100% quota never limits it;
 * otherwise the used part of the threshold (the net flow floored at 0) with
 * a note when the net flow went the other way.
 */
export function directionView(l: RateLimit, side: "send" | "recv"): DirectionView {
  const d: RateLimitDirection = l[side];
  const label = side === "send" ? "Out" : "In";
  const sym = limitSymbol(l);
  const noun = side === "send" ? "Outgoing" : "Incoming";
  if (isZero(d.max_percent)) {
    return { kind: "blocked", label, text: `${noun} transfers of this token are not allowed on this route` };
  }
  if (isFull(d.max_percent)) {
    return { kind: "unlimited", label, text: "No practical limit" };
  }
  if (!d.enforced) {
    return { kind: "unenforced", label, text: "Not enforced yet: the token has no supply to measure against" };
  }
  const pct = d.used_pct ?? 0;
  const net = new BigNumber(d.net_flow);
  const note = net.isNegative() ? `net inflow ${formatAmount(net.negated().toFixed(0), l.exponent, sym, { compact: true })}, room above the limit` : null;
  return {
    kind: "bar",
    label,
    text: `${formatAmount(d.used, l.exponent, undefined, { compact: true })} of ${formatAmount(d.threshold, l.exponent, sym, { compact: true })} used (${formatPct(pct, { trim: true })})`,
    pct: Math.min(Math.max(pct, 0), 100),
    tone: barTone(pct),
    note,
  };
}

/** "resets in 5 h 12 m (00:00 UTC)", or "resets every 24 h" when the next reset is not known. */
export function resetText(l: Pick<RateLimit, "next_reset" | "window_hours">, now: Date = new Date()): string {
  if (!l.next_reset) return `resets every ${l.window_hours} h`;
  const at = new Date(l.next_reset);
  const minutes = Math.max(0, Math.ceil((at.getTime() - now.getTime()) / 60000));
  const left = minutes >= 60 ? `${Math.floor(minutes / 60)} h ${minutes % 60} m` : `${minutes} m`;
  const clock = `${String(at.getUTCHours()).padStart(2, "0")}:${String(at.getUTCMinutes()).padStart(2, "0")} UTC`;
  return `resets in ${left} (${clock})`;
}

/** The limits grouped by channel, in the answer's order. */
export function limitsByChannel(items: RateLimit[]): Map<string, RateLimit[]> {
  const out = new Map<string, RateLimit[]>();
  for (const l of items) out.set(l.channel_id, [...(out.get(l.channel_id) ?? []), l]);
  return out;
}
