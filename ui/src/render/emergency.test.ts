import { describe, expect, it } from "vitest";

import type { Emergency } from "@/api/types";

import active from "../../test/fixtures/variants/emergency-active.json";
import { bannerParts, bannerText, guardianDao, haltCountdown, haltReached, measureCount, tokenSwitches } from "./emergency";

const e = active as Emergency;

describe("emergency render", () => {
  it("counts the switches in force", () => {
    expect(measureCount(e)).toBe(6);
    expect(measureCount({ ...e, halted_denoms: [], plan: null })).toBe(4);
  });

  it("caps the banner at 3 parts", () => {
    expect(bannerText(bannerParts(e))).toBe(
      "trading halted for 2 tokens · 1 token blocked from crossing chains · 1 channel closed + 3 more",
    );
    expect(bannerParts(e, true)).toHaveLength(5);
  });

  it("knows the halt was reached from the height or the live watch", () => {
    expect(haltReached({ height: 25120899, plan: e.plan }, false)).toBe(false);
    expect(haltReached({ height: 25120900, plan: e.plan }, false)).toBe(true);
    expect(haltReached({ height: 1, plan: e.plan }, true)).toBe(true);
    expect(haltReached({ height: 99, plan: null }, false)).toBe(false);
  });

  it("tells the time to a halt, or nothing when it cannot", () => {
    expect(haltCountdown(e, 5000)).toBe("in about 37 min (450 blocks)");
    expect(haltCountdown(e, null)).toBeNull();
    expect(haltCountdown({ ...e, height: 25121000 }, 5000)).toBeNull();
  });

  it("reads a DAO guardian from its label", () => {
    const g = (label: string | null, label_kind: string | null) => ({ address: "bze1x", label, label_kind });
    expect(guardianDao(g("DAO #3 · Ops", "dao"))).toEqual({ id: 3, name: "Ops" });
    expect(guardianDao(g("DAO #3", "dao"))).toEqual({ id: 3, name: null });
    expect(guardianDao(g("DAO #3 · Ops", "known"))).toBeNull();
    expect(guardianDao(g("Emergency key", "dao"))).toBeNull();
    expect(guardianDao(g(null, null))).toBeNull();
    expect(guardianDao(null)).toBeNull();
  });

  it("finds the switches of one token", () => {
    expect(tokenSwitches(e, "ibc/ABCDEF")).toEqual({ halted: false, ibcBlocked: true });
    expect(tokenSwitches(e, "factory/bze1abc/uusdc")).toEqual({ halted: true, ibcBlocked: false });
    expect(tokenSwitches(undefined, "x")).toEqual({ halted: false, ibcBlocked: false });
  });
});
