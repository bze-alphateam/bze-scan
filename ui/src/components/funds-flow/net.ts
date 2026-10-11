import BigNumber from "bignumber.js";

import type { Transfer } from "@/api/types";
import { formatAmount } from "@/lib/format";

/** A signed change of one denom, base units. */
export interface Delta {
  denom: string;
  amount: string;
  symbol: string | null;
  exponent: number | null;
}

export interface Net {
  /** What the address gained (positive) or lost (negative) per denom, the fee left out; zero deltas dropped. */
  deltas: Delta[];
  /** The fee the address paid per denom, positive. */
  fees: Delta[];
}

function add(into: Map<string, Delta>, t: Transfer, amount: BigNumber) {
  const prev = into.get(t.denom);
  const sum = new BigNumber(prev?.amount ?? 0).plus(amount);
  into.set(t.denom, { denom: t.denom, amount: sum.toFixed(), symbol: t.symbol, exponent: t.exponent });
}

/** An address's net movement over a transaction's transfers (fee transfers have a null `msg_index`). */
export function netFor(address: string, transfers: Transfer[]): Net {
  const deltas = new Map<string, Delta>();
  const fees = new Map<string, Delta>();
  for (const t of transfers) {
    const amount = new BigNumber(t.amount);
    if (!amount.isFinite()) continue;
    if (t.msg_index === null) {
      if (t.sender === address) add(fees, t, amount);
      continue;
    }
    if (t.recipient === address) add(deltas, t, amount);
    if (t.sender === address) add(deltas, t, amount.negated());
  }
  return {
    deltas: [...deltas.values()].filter((d) => !new BigNumber(d.amount).isZero()),
    fees: [...fees.values()],
  };
}

/** `+10 BZE`, `-0.5 USDC`; the raw amount and denom when unknown. */
export function deltaText(d: Delta): string {
  const n = new BigNumber(d.amount);
  const sign = n.isNegative() ? "-" : "+";
  const abs = n.abs().toFixed();
  const text = d.symbol != null && d.exponent != null ? formatAmount(abs, d.exponent, d.symbol) : `${abs} ${d.denom}`;
  return `${sign}${text}`;
}

/** Collapsed by default: a single action moving at most two transfers (typically the amount and the fee). */
export function isSimple(actionCount: number, transferCount: number): boolean {
  return actionCount <= 1 && transferCount <= 2;
}
