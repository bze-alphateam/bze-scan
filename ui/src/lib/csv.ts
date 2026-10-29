import BigNumber from "bignumber.js";

// CSV export helpers (BFE-279): comma-delimited, RFC 4180 quoting, CRLF line
// ends. Values are what spreadsheets read without help: UTC times as
// `YYYY-MM-DD HH:mm:ss`, plain decimal numbers with a dot and no grouping.

export type CsvValue = string | number | boolean | null | undefined;

const NEEDS_QUOTES = /[",\r\n]/;

/** One field: quoted when it holds a comma, a quote or a line break, quotes doubled. */
export function csvField(value: CsvValue): string {
  if (value == null) return "";
  const text = typeof value === "boolean" ? (value ? "yes" : "no") : String(value);
  return NEEDS_QUOTES.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

/** The whole file: the header row, then one line per row, each line ended by CRLF. */
export function toCsv(headers: string[], rows: CsvValue[][]): string {
  return [headers, ...rows].map((r) => r.map(csvField).join(",") + "\r\n").join("");
}

const pad = (n: number) => String(n).padStart(2, "0");

/** `2026-10-12 09:41:00`, in UTC; empty for a time that does not parse. */
export function csvTime(time: string | Date): string {
  const d = new Date(time);
  if (Number.isNaN(d.getTime())) return "";
  return (
    `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ` +
    `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`
  );
}

/** A base-unit amount in display units, exactly: `csvAmount("-1234500000", 6)` is `-1234.5`. */
export function csvAmount(base: string, exponent: number): string {
  const units = new BigNumber(base).shiftedBy(-exponent);
  return units.isFinite() ? units.toFixed() : base;
}

/** `transactions-bze1abc-20261029-142233.csv`: what, whose, when (UTC). */
export function csvFileName(what: string, subject: string, now: Date): string {
  const safe = subject.replace(/[^A-Za-z0-9._-]+/g, "_");
  const stamp =
    `${now.getUTCFullYear()}${pad(now.getUTCMonth() + 1)}${pad(now.getUTCDate())}-` +
    `${pad(now.getUTCHours())}${pad(now.getUTCMinutes())}${pad(now.getUTCSeconds())}`;
  return `${what}-${safe}-${stamp}.csv`;
}
