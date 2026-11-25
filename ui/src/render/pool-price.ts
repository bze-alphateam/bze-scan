import type { PoolAsset, PoolPrice } from "@/api/pool-price";
import { formatPct } from "@/lib/format";

const NATIVE = "ubze";

/** One plain-word line about the pool price, with the tone the card colours it in. */
export interface PoolStatusLine {
  key: "fees" | "module" | "ok" | "unknown";
  tone: "warn" | "ok" | "muted";
  text: string;
}

function assetName(a: PoolAsset): string {
  return a.symbol ?? a.denom;
}

/** The pool's token that is not BZE (the side whose fees are at stake); the base for a pool without BZE. */
export function poolTicker(pool: PoolPrice): string {
  const { base, quote } = pool.reserves;
  return assetName(base.denom === NATIVE ? quote : base);
}

/** A pool price for display: the display-unit price when the exponents are known, else the chain-unit one. */
export function poolPriceText(pool: PoolPrice, which: "price" | "avg_price"): string | null {
  const p = pool[which];
  if (!p) return null;
  return p.display ?? p.chain;
}

/** "BZE per HONEY"-style unit of the pool's price: quote per base. */
export function poolPriceUnit(pool: PoolPrice): string {
  return `${assetName(pool.reserves.quote)} per ${assetName(pool.reserves.base)}`;
}

const pct = (v: string | null) => (v == null ? "" : formatPct(v, { trim: true }));

/** The status lines of the pool price card: none when the node has no average for the pool. */
export function poolStatus(pool: PoolPrice): PoolStatusLine[] {
  if (pool.avg_price == null) {
    return [{ key: "unknown", tone: "muted", text: "The chain has no recent average for this pool yet." }];
  }
  if (pool.fees_in_bze == null || pool.module_swaps_held == null) {
    return []; // a pool without BZE: the average and the drift are shown, no rule applies
  }
  const lines: PoolStatusLine[] = [];
  const away = pool.drift_pct != null ? `${pct(pool.drift_pct)} away from` : "not comparable with";
  if (pool.fees_in_bze) {
    lines.push({
      key: "fees",
      tone: "warn",
      text: `Fees paid in ${poolTicker(pool)} are charged in BZE for now: the pool price is ${away} its recent average (limit ${pct(pool.fee_max_drift_pct)}).`,
    });
  }
  if (pool.module_swaps_held) {
    lines.push({
      key: "module",
      tone: "warn",
      text: `Automatic fee conversion and burner moves through this pool are on hold: more than ${pct(pool.module_max_drift_pct)} away.`,
    });
  }
  if (lines.length === 0) {
    lines.push({ key: "ok", tone: "ok", text: "Within limits." });
  }
  return lines;
}
