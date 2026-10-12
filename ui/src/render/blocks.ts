import BigNumber from "bignumber.js";

import type { Block, Coin, Status } from "@/api/types";
import { formatCount } from "@/lib/format";

/**
 * Why the explorer has no block at `height`: above its latest block it is
 * `not_produced` (yet), below the oldest block the backfill reached it is
 * `too_old`, otherwise it is `missing` (a gap). Without the status it is
 * `missing`.
 */
export type BlockNotFound = "not_produced" | "too_old" | "missing";

export function blockNotFound(height: number, status: Status | undefined): BlockNotFound {
  const tip = status?.live_fill.db_height;
  if (tip != null && height > tip) return "not_produced";
  const oldest = status?.back_fill.oldest_height;
  if (oldest != null && height < oldest) return "too_old";
  return "missing";
}

/** Per-denom sums of coin lists, denoms in order of first appearance, zero sums dropped. */
export function sumCoins(lists: Coin[][]): Coin[] {
  const sums = new Map<string, BigNumber>();
  for (const list of lists) {
    for (const c of list) {
      const n = new BigNumber(c.amount);
      if (!n.isFinite()) continue;
      sums.set(c.denom, (sums.get(c.denom) ?? new BigNumber(0)).plus(n));
    }
  }
  return [...sums].filter(([, n]) => !n.isZero()).map(([denom, n]) => ({ denom, amount: n.toFixed() }));
}

/** The fees the block's transactions paid, per denom, and how many transactions paid one. */
export function blockFees(block: Pick<Block, "transactions">): { coins: Coin[]; payers: number } {
  const paid = block.transactions.filter((t) => t.fee.length > 0);
  return { coins: sumCoins(paid.map((t) => t.fee)), payers: paid.length };
}

/** The gas the block's transactions used; null when no transaction reports it. */
export function blockGas(block: Pick<Block, "transactions">): number | null {
  const known = block.transactions.map((t) => t.gas_used).filter((g): g is number => g != null);
  return known.length === 0 ? null : known.reduce((a, b) => a + b, 0);
}

/** `5.8 s` from milliseconds. */
export function blockTimeText(ms: number | null): string {
  return ms == null ? "—" : `${(ms / 1000).toFixed(1)} s`;
}

/** `4 KB`, `1.5 KB` from bytes (one decimal, trailing `.0` dropped). */
export function sizeText(bytes: number | null): string {
  return bytes == null ? "—" : `${formatCount((bytes / 1024).toFixed(1)).replace(/\.0$/, "")} KB`;
}
