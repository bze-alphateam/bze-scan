import BigNumber from "bignumber.js";

// Decimal math on the API's string amounts, so no page does float math and
// `bignumber.js` stays inside `src/lib`. Every function takes and returns
// plain strings; an unreadable input counts as zero.

const num = (v: string | number | null | undefined) => {
  const n = new BigNumber(v ?? 0);
  return n.isFinite() ? n : new BigNumber(0);
};

/** The sum of base-unit amounts: `sumAmounts(["1", "2"])` is `"3"`. */
export function sumAmounts(values: (string | number)[]): string {
  return values.reduce<BigNumber>((acc, v) => acc.plus(num(v)), new BigNumber(0)).toFixed();
}

/** True when the amount is above zero (`isPositive()` is also true for zero). */
export function isAboveZero(value: string | number | null | undefined): boolean {
  return num(value).gt(0);
}

/** A base-unit amount's USD value as a decimal string; null without a price. */
export function usdValue(base: string, exponent: number, priceUsd: string | null | undefined): string | null {
  if (priceUsd == null) return null;
  const price = new BigNumber(priceUsd);
  if (!price.isFinite()) return null;
  return num(base).shiftedBy(-exponent).times(price).toFixed();
}

/** `part` as a percentage of `total` ("25" for a quarter); null when the total is zero. */
export function percentOf(part: string, total: string | null | undefined): string | null {
  const t = num(total);
  return t.gt(0) ? num(part).div(t).times(100).toFixed() : null;
}

/** Orders two decimal strings: negative when `a` is smaller. */
export function compareDecimals(a: string, b: string): number {
  return num(a).comparedTo(num(b)) ?? 0;
}
