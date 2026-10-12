import BigNumber from "bignumber.js";

import type { Stats } from "@/api/types";
import { formatCount, formatPct, formatUsd } from "@/lib/format";
import { blockTimeText } from "@/render/blocks";

/** A home tile's words; `time` is a timestamp the tile shows as relative time. */
export interface HomeTile {
  label: string;
  value: string | null;
  sub: string | null;
  time?: string | null;
  /** The price change's direction, for its colour. */
  tone?: "up" | "down" | "flat";
}

/** `+1.2% today`, `-0.4% today`, and which way it went; null without a change. */
export function priceChange(pct: string | null): { text: string; tone: "up" | "down" | "flat" } | null {
  if (pct == null) return null;
  const v = new BigNumber(pct);
  if (!v.isFinite()) return null;
  const text = formatPct(v.abs().toFixed());
  if (text === "0.0%") return { text: "0.0% today", tone: "flat" };
  return v.isPositive() ? { text: `+${text} today`, tone: "up" } : { text: `-${text} today`, tone: "down" };
}

/** The six home tiles from `/stats`; every value null (shown as "—") without stats. */
export function homeTiles(s: Stats | undefined): HomeTile[] {
  const change = priceChange(s?.price_change_24h_pct ?? null);
  const seats = s?.max_validators != null ? `active · ${formatCount(s.max_validators)} seats` : "active";
  return [
    { label: "Latest block", value: s?.latest_height != null ? formatCount(s.latest_height) : null, sub: null, time: s?.latest_time ?? null },
    { label: "Block time", value: s?.avg_block_time_ms != null ? blockTimeText(s.avg_block_time_ms) : null, sub: "average, last hour" },
    { label: "Transactions", value: s?.txs_24h != null ? formatCount(s.txs_24h) : null, sub: "last 24 hours" },
    { label: "Validators", value: s?.validators ? formatCount(s.validators.active) : null, sub: seats },
    { label: "Staked", value: s?.staked_share != null ? formatPct(s.staked_share) : null, sub: "of BZE supply" },
    s?.price_usd != null
      ? { label: "BZE price", value: formatUsd(s.price_usd).replace(/^≈ /, ""), sub: change?.text ?? null, tone: change?.tone }
      : { label: "BZE price", value: null, sub: "no price yet" },
  ];
}
