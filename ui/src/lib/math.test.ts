import { describe, expect, it } from "vitest";

import { compareDecimals, isAboveZero, percentOf, sumAmounts, usdValue } from "./math";

describe("math", () => {
  it("sums base-unit amounts beyond float precision", () => {
    expect(sumAmounts(["9007199254740993", "1"])).toBe("9007199254740994");
    expect(sumAmounts([])).toBe("0");
  });

  it("tells a zero from a positive amount", () => {
    expect(isAboveZero("0")).toBe(false);
    expect(isAboveZero("1")).toBe(true);
    expect(isAboveZero(null)).toBe(false);
  });

  it("values an amount at a price, null without one", () => {
    expect(usdValue("1234500000", 6, "0.0015")).toBe("1.85175");
    expect(usdValue("1", 6, null)).toBeNull();
    expect(usdValue("1", 6, "n/a")).toBeNull();
  });

  it("gives a share in percent, null of a zero total", () => {
    expect(percentOf("1", "3")).toMatch(/^33\.333/);
    expect(percentOf("1", "0")).toBeNull();
    expect(percentOf("1", null)).toBeNull();
  });

  it("orders decimal strings", () => {
    expect(compareDecimals("10", "9.5")).toBe(1);
    expect(compareDecimals("1", "1.0")).toBe(0);
  });
});
