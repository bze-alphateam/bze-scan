import type { PoolAsset, PoolPrice } from "@/api/pool-price";
import { formatPct } from "@/lib/format";

const NATIVE = "ubze";

/** One plain-word line about the pool price, with the tone the card colours it in. */
export interface PoolStatusLine {
  key: "fees" | "module" | "ok" | "unknown" | "unchecked";
  tone: "warn" | "ok" | "muted";
  text: string;
}

function assetName(a: PoolAsset): string {
  return a.symbol ?? a.denom;
}

/** Whether the pool has BZE on one side: only those pools are checked by the chain. */
export function hasNative(pool: PoolPrice): boolean {
  return pool.reserves.base.denom === NATIVE || pool.reserves.quote.denom === NATIVE;
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
  return p.display ?? trimZeros(p.chain);
}

/** "BZE per HONEY"-style unit of the pool's price: quote per base. */
export function poolPriceUnit(pool: PoolPrice): string {
  return `${assetName(pool.reserves.quote)} per ${assetName(pool.reserves.base)}`;
}

/** "2.000000000000000000" as "2"; a value without a fraction is left alone. */
function trimZeros(v: string): string {
  return v.includes(".") ? v.replace(/0+$/, "").replace(/\.$/, "") : v;
}

const pct = (v: string | null) => (v == null ? "" : formatPct(v, { trim: true }));

/** The status lines of the pool price card: none when the node has no average for the pool. */
export function poolStatus(pool: PoolPrice): PoolStatusLine[] {
  if (pool.avg_price == null) {
    return [{ key: "unknown", tone: "muted", text: "The chain has no recent average for this pool yet." }];
  }
  if (pool.fees_in_bze == null || pool.module_swaps_held == null) {
    // A pool without BZE has no rule to show. With BZE the flags are only null when the limits could not be read.
    return hasNative(pool) ? [{ key: "unchecked", tone: "muted", text: "The fee rules could not be checked for this pool right now." }] : [];
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
