import BigNumber from "bignumber.js";

// Pure display helpers (UI Design, "Formatting rules"). Every function takes
// plain values and returns a string; none reads the clock unless given `now`.

const GROUPING = {
  decimalSeparator: ".",
  groupSeparator: ",",
  groupSize: 3,
  prefix: "",
  suffix: "",
};

const COMPACT_UNITS: [number, string][] = [
  [1e12, "T"],
  [1e9, "B"],
  [1e6, "M"],
  [1e3, "K"],
];

/** Thousands separators, at most `decimals` decimals, trailing zeros trimmed (rounded down). */
function group(value: BigNumber, decimals: number): string {
  // Without a decimal count toFormat neither rounds nor pads.
  return value.decimalPlaces(decimals, BigNumber.ROUND_DOWN).toFormat(GROUPING);
}

/**
 * A base-unit amount in display units: `formatAmount("1234500000", 6, "BZE")`
 * is `1,234.5 BZE`. `compact` (tiles and list cells only) shortens large
 * values: `14.3M BZE`.
 */
export function formatAmount(
  base: string | number,
  exponent: number,
  symbol?: string | null,
  options: { compact?: boolean } = {},
): string {
  const units = new BigNumber(base).shiftedBy(-exponent);
  let text: string;
  if (!units.isFinite()) {
    text = String(base);
  } else if (options.compact && units.abs().gte(1e3)) {
    const [size, suffix] = COMPACT_UNITS.find(([size]) => units.abs().gte(size))!;
    text = group(units.dividedBy(size), 1) + suffix;
  } else if (options.compact) {
    const short = group(units, 2);
    text = short === "0" && !units.isZero() ? group(units, exponent) : short;
  } else {
    text = group(units, exponent);
  }
  return symbol ? `${text} ${symbol}` : text;
}

/** A USD value: `≈ $11.50`, four significant digits under one cent. */
export function formatUsd(value: string | number): string {
  const v = new BigNumber(value);
  if (!v.isFinite()) {
    return "";
  }
  const abs = v.abs();
  let text: string;
  if (!abs.isZero() && abs.lt(0.01)) {
    text = abs.precision(4, BigNumber.ROUND_HALF_UP).toFixed();
  } else {
    text = abs.toFormat(2, BigNumber.ROUND_HALF_UP, GROUPING);
  }
  return `≈ ${v.isNegative() ? "-" : ""}$${text}`;
}

/**
 * A percentage with one decimal: `formatPct("12.34567")` is `12.3%`. `trim`
 * drops a trailing `.0` (`5%`, `100%`); `round` picks the rounding
 * (half up by default; down where "100%" must mean all of it).
 */
export function formatPct(
  value: string | number,
  options: { trim?: boolean; round?: "half_up" | "down" } = {},
): string {
  const v = new BigNumber(value);
  if (!v.isFinite()) {
    return "";
  }
  const mode = options.round === "down" ? BigNumber.ROUND_DOWN : BigNumber.ROUND_HALF_UP;
  const text = v.toFormat(1, mode, GROUPING);
  return `${options.trim ? text.replace(/\.0$/, "") : text}%`;
}

/** A count with thousands separators. */
export function formatCount(value: string | number): string {
  const v = new BigNumber(value);
  return v.isFinite() ? v.toFormat(GROUPING) : String(value);
}

/** `3F9A…C21E`: the first and last four characters, upper case. */
export function shortHash(hash: string): string {
  const h = hash.toUpperCase();
  return h.length <= 10 ? h : `${h.slice(0, 4)}…${h.slice(-4)}`;
}

/** `bze1q8f…xu4a`: the bech32 prefix and separator, three characters, an ellipsis, the last four. */
export function shortAddress(address: string): string {
  const sep = address.lastIndexOf("1");
  if (sep <= 0) {
    return address.length <= 12 ? address : `${address.slice(0, 6)}…${address.slice(-4)}`;
  }
  const head = address.slice(0, sep + 4);
  return address.length <= head.length + 5 ? address : `${head}…${address.slice(-4)}`;
}

/**
 * A DEX order id as lists show it: ids longer than 8 characters keep the first
 * character and the last 7 (`0…6714623`); show the full id on hover.
 */
export function shortOrderId(id: string): string {
  return id.length <= 8 ? id : `${id[0]}…${id.slice(-7)}`;
}

const RELATIVE_STEPS: [number, Intl.RelativeTimeFormatUnit][] = [
  [60, "second"],
  [60, "minute"],
  [24, "hour"],
  [30, "day"],
  [12, "month"],
  [Infinity, "year"],
];

const relative = new Intl.RelativeTimeFormat("en", { numeric: "always" });

/** `12 seconds ago`, `3 hours ago`, `in 5 minutes`; `just now` within a second. */
export function relativeTime(time: string | Date, now: Date = new Date()): string {
  const seconds = Math.round((new Date(time).getTime() - now.getTime()) / 1000);
  if (Math.abs(seconds) < 1) {
    return "just now";
  }
  let value = seconds;
  for (const [size, unit] of RELATIVE_STEPS) {
    if (Math.abs(value) < size) {
      return relative.format(value, unit);
    }
    value = Math.trunc(value / size);
  }
  return relative.format(value, "year");
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const pad = (n: number) => String(n).padStart(2, "0");

/** `Oct 6, 2026, 14:32:11 UTC`; `Oct 6, 2026, 14:32 UTC` without `seconds`. */
export function formatDateTime(time: string | Date, { seconds = true }: { seconds?: boolean } = {}): string {
  const d = new Date(time);
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  return (
    `${MONTHS[d.getUTCMonth()]} ${d.getUTCDate()}, ${d.getUTCFullYear()}, ` +
    `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}${seconds ? `:${pad(d.getUTCSeconds())}` : ""} UTC`
  );
}

/** `Oct 6, 2026` (UTC). */
export function formatDate(time: string | Date): string {
  const d = new Date(time);
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  return `${MONTHS[d.getUTCMonth()]} ${d.getUTCDate()}, ${d.getUTCFullYear()}`;
}

/** `1st`, `2nd`, `3rd`, `11th`, `22nd`. */
export function ordinal(n: number): string {
  const tens = n % 100;
  if (tens >= 11 && tens <= 13) return `${n}th`;
  return `${n}${["th", "st", "nd", "rd"][n % 10] ?? "th"}`;
}

const BECH32 = /^[a-z]+1[02-9ac-hj-np-z]{6,}$/;

/**
 * Two letters for an avatar: the initials of a moniker's first two words, or
 * its first two letters; for a bech32 address the two characters after the
 * prefix.
 */
export function initials(value: string): string {
  const v = value.trim();
  if (BECH32.test(v)) {
    const sep = v.lastIndexOf("1");
    return v.slice(sep + 1, sep + 3).toUpperCase();
  }
  const words = v.split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  if (words.length === 0) {
    return "?";
  }
  if (words.length === 1) {
    return Array.from(words[0]).slice(0, 2).join("").toUpperCase();
  }
  return (Array.from(words[0])[0] + Array.from(words[1])[0]).toUpperCase();
}

/** The number of colours in the avatar palette (the `avatar.0`…`avatar.7` tokens). */
export const AVATAR_COLORS = 8;

/** An avatar palette index (0–7) derived from the string, the same for the same string. */
export function avatarIndex(value: string): number {
  let hash = 0;
  for (const c of value) {
    hash = (hash * 31 + c.codePointAt(0)!) >>> 0;
  }
  return hash % AVATAR_COLORS;
}
