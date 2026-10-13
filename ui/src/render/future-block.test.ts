import { describe, expect, it } from "vitest";

import type { BlockTimeWindow, Stats } from "@/api/types";

import {
  byDate,
  byHeight,
  countdown,
  coversText,
  dateInputs,
  dateParam,
  durationText,
  estimator,
  formatLocal,
  MIN_MARGIN_MS,
  parseDateTime,
  parseHeight,
  tipAt,
  type Estimator,
} from "./future-block";

const TIME = "2026-10-11T12:00:00Z";
const T0 = Date.parse(TIME);

const windows = (avgs: (number | null)[]): BlockTimeWindow[] =>
  [10_000, 30_000, 50_000, 100_000].map((blocks, i) => ({ blocks, from_height: 25_120_450 - blocks, avg_block_time_ms: avgs[i] }));

type StatsPart = Pick<Stats, "latest_height" | "latest_time" | "avg_block_time_ms" | "block_time_windows">;
const stats = (over: Partial<StatsPart> = {}): StatsPart => ({
  latest_height: 25_120_450,
  latest_time: TIME,
  avg_block_time_ms: 5900,
  block_time_windows: windows([5810, 5800, 5790, 5800]),
  ...over,
});

const fixed = (over: Partial<Estimator> = {}): Estimator => ({ bt: 5800, window: 100_000, spread: 0, height: 25_120_450, time: T0, ...over });

describe("estimator", () => {
  it("uses the 100,000-block average and the spread of all four", () => {
    const e = estimator(stats());
    expect(e).toEqual({ bt: 5800, window: 100_000, spread: 20 / 5800, height: 25_120_450, time: T0 });
  });

  it("falls back to the longest known window when the 100,000-block one is null", () => {
    expect(estimator(stats({ block_time_windows: windows([5810, 5800, 5790, null]) }))).toMatchObject({ bt: 5790, window: 50_000 });
  });

  it("falls back to the plain average without any window, with no spread", () => {
    expect(estimator(stats({ block_time_windows: windows([null, null, null, null]) }))).toMatchObject({ bt: 5900, window: null, spread: 0 });
    expect(estimator(stats({ block_time_windows: [] }))).toMatchObject({ bt: 5900, window: null });
  });

  it("has nothing to estimate from without any average or a latest block", () => {
    expect(estimator(stats({ block_time_windows: windows([null, null, null, null]), avg_block_time_ms: null }))).toBeNull();
    expect(estimator(stats({ latest_height: null }))).toBeNull();
    expect(estimator(stats({ latest_time: null }))).toBeNull();
  });

  it("has no spread with a single known window", () => {
    expect(estimator(stats({ block_time_windows: windows([5810, null, null, null]) }))).toMatchObject({ bt: 5810, window: 10_000, spread: 0 });
  });
});

describe("tipAt", () => {
  it("extrapolates whole blocks since the latest one", () => {
    const e = fixed();
    expect(tipAt(e, T0)).toBe(25_120_450);
    expect(tipAt(e, T0 + 10 * 5800 - 1)).toBe(25_120_459);
    expect(tipAt(e, T0 + 10 * 5800)).toBe(25_120_460);
  });

  it("never goes below the latest block when the clock is behind", () => {
    expect(tipAt(fixed(), T0 - 60_000)).toBe(25_120_450);
  });
});

describe("byHeight", () => {
  it("says a height at or below the tip is produced", () => {
    expect(byHeight(fixed(), 25_120_450, T0)).toEqual({ kind: "past" });
    expect(byHeight(fixed(), 25_120_460, T0 + 10 * 5800)).toEqual({ kind: "past" });
  });

  it("counts the blocks left and anchors the date on the latest block", () => {
    const e = fixed({ spread: 20 / 5800 });
    const r = byHeight(e, 25_135_000, T0);
    expect(r).toEqual({ kind: "future", left: 14_550, eta: T0 + 14_550 * 5800, marginMs: 14_550 * 5800 * (20 / 5800) });
    // A few seconds later the date stays put; only the tip and the margin move.
    const later = byHeight(e, 25_135_000, T0 + 3000);
    expect(later).toMatchObject({ left: 14_550, eta: T0 + 14_550 * 5800 });
  });

  it("gives or takes at least a minute", () => {
    expect(byHeight(fixed(), 25_120_460, T0)).toMatchObject({ marginMs: MIN_MARGIN_MS });
    expect(byHeight(fixed({ spread: 0.001 }), 25_120_451, T0)).toMatchObject({ marginMs: MIN_MARGIN_MS });
  });
});

describe("byDate", () => {
  it("rounds the height the chain should be at", () => {
    const e = fixed();
    expect(byDate(e, T0 + 2.4 * 5800, T0)).toMatchObject({ height: 25_120_452, inMs: 2.4 * 5800 });
    expect(byDate(e, T0 + 2.6 * 5800, T0)).toMatchObject({ height: 25_120_453 });
  });

  it("gives or takes the spread of the blocks to go, at least one", () => {
    expect(byDate(fixed(), T0 + 86_400_000, T0)).toMatchObject({ marginBlocks: 1 });
    const e = fixed({ spread: 0.01 });
    const r = byDate(e, T0 + 1000 * 5800, T0);
    expect(r).toMatchObject({ height: 25_121_450, marginBlocks: 10 });
  });

  it("has no answer for a date not in the future", () => {
    expect(byDate(fixed(), T0, T0)).toBeNull();
    expect(byDate(fixed(), T0 - 1, T0)).toBeNull();
  });
});

describe("parseHeight", () => {
  it.each([
    ["25135000", 25_135_000],
    ["25,135,000", 25_135_000],
    [" 25 135 000 ", 25_135_000],
    ["1", 1],
  ])("reads %j", (text, want) => {
    expect(parseHeight(text)).toBe(want);
  });

  it.each(["", "0", "abc", "25,13,5000", "-5", "1.5", "99999999999999999999"])("rejects %j", (text) => {
    expect(parseHeight(text)).toBeNull();
  });
});

describe("parseDateTime", () => {
  it("reads day.month.year and a 24-hour time as local time", () => {
    expect(parseDateTime("18.10.2026", "05:00")).toBe(new Date(2026, 9, 18, 5, 0).getTime());
    expect(parseDateTime("1/2/2027", "23:59")).toBe(new Date(2027, 1, 1, 23, 59).getTime());
  });

  it.each([
    ["31.02.2026", "05:00"],
    ["18.13.2026", "05:00"],
    ["18.10.2026", "24:00"],
    ["18.10.2026", "05:60"],
    ["2026-10-18", "05:00"],
    ["18.10.2026", "5pm"],
    ["", ""],
  ])("rejects %j %j", (date, time) => {
    expect(parseDateTime(date, time)).toBeNull();
  });
});

describe("the date inputs", () => {
  it("reads ?date= into the two inputs", () => {
    expect(dateParam("2026-10-18T05:00")).toEqual({ date: "18.10.2026", time: "05:00" });
    expect(dateParam("18.10.2026")).toBeNull();
  });

  it("writes a moment as the two inputs, local time", () => {
    expect(dateInputs(new Date(2026, 0, 5, 7, 3).getTime())).toEqual({ date: "05.01.2026", time: "07:03" });
  });
});

describe("text", () => {
  it("splits the countdown into two-digit units", () => {
    expect(countdown(((2 * 24 + 3) * 3600 + 4 * 60 + 5) * 1000 + 999)).toEqual({ days: "02", hours: "03", minutes: "04", seconds: "05" });
    expect(countdown(-5000)).toEqual({ days: "00", hours: "00", minutes: "00", seconds: "00" });
    expect(countdown(120 * 86_400_000).days).toBe("120");
  });

  it("writes durations in days and hours, hours and minutes, or minutes", () => {
    expect(durationText((5 * 86_400 + 3 * 3600 + 59) * 1000)).toBe("5 d 3 h");
    expect(durationText((4 * 3600 + 12 * 60) * 1000)).toBe("4 h 12 min");
    expect(durationText(291_000)).toBe("4 min");
    expect(durationText(10_000)).toBe("1 min");
  });

  it("writes what a window covers", () => {
    expect(coversText(10_000, 5810)).toBe("16 hours");
    expect(coversText(30_000, 5800)).toBe("2 days");
    expect(coversText(50_000, 5790)).toBe("3.4 days");
    expect(coversText(100_000, 5800)).toBe("6.7 days");
    expect(coversText(100, 6000)).toBe("1 hour");
  });

  it("writes a local date in 24-hour time without seconds", () => {
    expect(formatLocal(new Date(2026, 9, 18, 17, 5, 59).getTime())).toBe("Oct 18, 2026, 17:05");
  });
});
