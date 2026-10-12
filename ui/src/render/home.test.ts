import { describe, expect, it } from "vitest";

import type { Stats } from "@/api/types";

import stats from "../../test/fixtures/api/v1/stats.json";

import { homeTiles, priceChange } from "./home";

const s = stats as Stats;

describe("homeTiles", () => {
  it("reads the six tiles from the stats", () => {
    expect(homeTiles(s).map(({ label, value, sub, tone }) => [label, value, sub, tone])).toEqual([
      ["Latest block", "25,120,450", null, undefined],
      ["Block time", "5.9 s", "average, last hour", undefined],
      ["Transactions", "1,840", "last 24 hours", undefined],
      ["Validators", "3", "active · 40 seats", undefined],
      ["Staked", "61.2%", "of BZE supply", undefined],
      ["BZE price", "$0.0015", "+1.2% today", "up"],
    ]);
    expect(homeTiles(s)[0].time).toBe(s.latest_time);
  });

  it("says no price yet without one, and leaves every value empty without stats", () => {
    expect(homeTiles({ ...s, price_usd: null, price_change_24h_pct: null })[5]).toEqual({ label: "BZE price", value: null, sub: "no price yet" });
    expect(homeTiles(undefined).map((t) => t.value)).toEqual([null, null, null, null, null, null]);
  });
});

describe("priceChange", () => {
  it("signs and colours the day's change", () => {
    expect(priceChange("-0.43210")).toEqual({ text: "-0.4% today", tone: "down" });
    expect(priceChange("0.01")).toEqual({ text: "0.0% today", tone: "flat" });
    expect(priceChange(null)).toBeNull();
  });
});
