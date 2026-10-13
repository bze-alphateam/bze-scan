import type { Stats } from "@/api/types";

// The Future block page's maths: every function is pure and takes `now`
// (epoch ms), so the countdown and the tests drive the clock.

/** The window the estimate prefers: the longest, steadiest average. */
export const PREFERRED_WINDOW = 100_000;

export interface Estimator {
  /** The block time the estimate uses, in ms. */
  bt: number;
  /** The window `bt` comes from, in blocks; null when it is the stats' plain average. */
  window: number | null;
  /** How far the windows' averages differ, as a share of `bt` (0 with a single window). */
  spread: number;
  /** The latest indexed block and its time (epoch ms): the anchor of every extrapolation. */
  height: number;
  time: number;
}

/**
 * The estimator from `/stats`: the 100,000-block average, else the longest
 * known window, else the plain average; null without a latest block or any
 * average ("Not enough history to estimate yet").
 */
export function estimator(stats: Pick<Stats, "latest_height" | "latest_time" | "avg_block_time_ms" | "block_time_windows">): Estimator | null {
  if (stats.latest_height == null || stats.latest_time == null) return null;
  const time = new Date(stats.latest_time).getTime();
  if (Number.isNaN(time)) return null;
  const known = (stats.block_time_windows ?? []).filter((w): w is typeof w & { avg_block_time_ms: number } => w.avg_block_time_ms != null && w.avg_block_time_ms > 0);
  const preferred = known.find((w) => w.blocks === PREFERRED_WINDOW);
  const longest = known.reduce<(typeof known)[number] | null>((best, w) => (best == null || w.blocks > best.blocks ? w : best), null);
  const chosen = preferred ?? longest;
  const bt = chosen?.avg_block_time_ms ?? stats.avg_block_time_ms;
  if (bt == null || !(bt > 0)) return null;
  const avgs = known.map((w) => w.avg_block_time_ms);
  const spread = avgs.length > 1 ? (Math.max(...avgs) - Math.min(...avgs)) / bt : 0;
  return { bt, window: chosen?.blocks ?? null, spread, height: stats.latest_height, time };
}

/** The chain's height now, extrapolated from the latest indexed block (never below it). */
export function tipAt(e: Estimator, now: number): number {
  return e.height + Math.max(0, Math.floor((now - e.time) / e.bt));
}

/** The least "give or take" of a countdown: a minute. */
export const MIN_MARGIN_MS = 60_000;

export type HeightEstimate = { kind: "past" } | { kind: "future"; left: number; eta: number; marginMs: number };

/**
 * When block `height` is produced: past at or below the tip, else the blocks
 * left, the date (anchored on the latest block, so it does not move as the
 * tip ticks) and the give or take, the spread applied to the time left.
 */
export function byHeight(e: Estimator, height: number, now: number): HeightEstimate {
  const tip = tipAt(e, now);
  if (height <= tip) return { kind: "past" };
  const eta = e.time + (height - e.height) * e.bt;
  return { kind: "future", left: height - tip, eta, marginMs: Math.max(MIN_MARGIN_MS, (eta - now) * e.spread) };
}

export interface DateEstimate {
  height: number;
  /** The give or take in blocks, at least one. */
  marginBlocks: number;
  /** The time from now to the date, in ms. */
  inMs: number;
}

/** The block the chain should be at on `date` (epoch ms); null when the date is not in the future. */
export function byDate(e: Estimator, date: number, now: number): DateEstimate | null {
  if (!(date > now)) return null;
  const height = Math.max(tipAt(e, now), Math.round(e.height + (date - e.time) / e.bt));
  return { height, marginBlocks: Math.max(1, Math.round((height - tipAt(e, now)) * e.spread)), inMs: date - now };
}

/** A typed block height: digits with optional thousands commas or spaces; null otherwise. */
export function parseHeight(text: string): number | null {
  const t = text.trim();
  if (!/^\d{1,3}([, ]?\d{3})*$/.test(t)) return null;
  const n = Number(t.replace(/[, ]/g, ""));
  return Number.isSafeInteger(n) && n > 0 ? n : null;
}

/** `18.10.2026` + `05:00` (24-hour, local time) as epoch ms; null when either is not a real date or time. */
export function parseDateTime(date: string, time: string): number | null {
  const d = /^(\d{1,2})[./-](\d{1,2})[./-](\d{4})$/.exec(date.trim());
  const t = /^(\d{1,2}):(\d{2})$/.exec(time.trim());
  if (!d || !t) return null;
  const [day, month, year, hour, minute] = [d[1], d[2], d[3], t[1], t[2]].map(Number);
  if (hour > 23 || minute > 59) return null;
  const at = new Date(year, month - 1, day, hour, minute);
  // 31.02 rolls over into March: a real date keeps its parts.
  if (at.getFullYear() !== year || at.getMonth() !== month - 1 || at.getDate() !== day) return null;
  return at.getTime();
}

/** `?date=2026-10-18T05:00` (local time) as the two inputs' text; null when malformed. */
export function dateParam(value: string): { date: string; time: string } | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value);
  if (!m) return null;
  return { date: `${m[3]}.${m[2]}.${m[1]}`, time: `${m[4]}:${m[5]}` };
}

const pad = (n: number) => String(n).padStart(2, "0");

/** The inputs' text for a moment: `18.10.2026` and `05:00`, local time. */
export function dateInputs(at: number): { date: string; time: string } {
  const d = new Date(at);
  return { date: `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()}`, time: `${pad(d.getHours())}:${pad(d.getMinutes())}` };
}

/** The countdown boxes: days, hours, minutes and seconds left, two digits each (days may be more). */
export function countdown(ms: number): { days: string; hours: string; minutes: string; seconds: string } {
  let s = Math.max(0, Math.floor(ms / 1000));
  const days = Math.floor(s / 86_400);
  s -= days * 86_400;
  const hours = Math.floor(s / 3600);
  s -= hours * 3600;
  const minutes = Math.floor(s / 60);
  return { days: pad(days), hours: pad(hours), minutes: pad(minutes), seconds: pad(s - minutes * 60) };
}

/** A span in words: `5 d 3 h`, `4 h 12 min`, `7 min` (at least a minute). */
export function durationText(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  const d = Math.floor(s / 86_400);
  const h = Math.floor((s % 86_400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d} d ${h} h`;
  if (h > 0) return `${h} h ${m} min`;
  return `${Math.max(1, m)} min`;
}

/** How long a window lasts: `16 hours` under a day, else `2 days`, `3.4 days`. */
export function coversText(blocks: number, avgMs: number): string {
  const hours = (blocks * avgMs) / 3_600_000;
  if (hours < 24) {
    const h = Math.max(1, Math.round(hours));
    return `${h} ${h === 1 ? "hour" : "hours"}`;
  }
  const days = Math.round((hours / 24) * 10) / 10;
  return `${days} ${days === 1 ? "day" : "days"}`;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** `Oct 18, 2026, 05:00` in the viewer's time zone (24-hour, no seconds: it is an estimate). */
export function formatLocal(at: number): string {
  const d = new Date(at);
  return `${MONTHS[d.getMonth()]} ${d.getDate()}, ${d.getFullYear()}, ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
