import { describe, expect, it } from "vitest";

import type { PoolPrice } from "@/api/pool-price";

import { poolPriceText, poolPriceUnit, poolStatus, poolTicker } from "./pool-price";

const asset = (denom: string, symbol: string | null) => ({ denom, symbol, exponent: 6, reserve: "1000" });

function pool(over: Partial<PoolPrice> = {}): PoolPrice {
  return {
    pool_id: "uhoney_ubze",
    reserves: { base: asset("uhoney", "HONEY"), quote: asset("ubze", "BZE") },
    price: { chain: "2.000000000000000000", display: "2" },
    avg_price: { chain: "1.800000000000000000", display: "1.8" },
    avg_last_height: 10,
    drift_pct: "11.1",
    half_life_blocks: 20,
    fee_max_drift_pct: "10",
    module_max_drift_pct: "3",
    fees_in_bze: true,
    module_swaps_held: true,
    ...over,
  };
}

describe("poolStatus", () => {
  it("says fees are charged in BZE and the module moves are on hold when past both limits", () => {
    expect(poolStatus(pool()).map((l) => l.text)).toEqual([
      "Fees paid in HONEY are charged in BZE for now: the pool price is 11.1% away from its recent average (limit 10%).",
      "Automatic fee conversion and burner moves through this pool are on hold: more than 3% away.",
    ]);
  });

  it("says only the module moves are on hold between the two limits", () => {
    const lines = poolStatus(pool({ drift_pct: "5.0", fees_in_bze: false }));
    expect(lines.map((l) => l.key)).toEqual(["module"]);
  });

  it("says within limits when neither flag is set", () => {
    expect(poolStatus(pool({ drift_pct: "1.0", fees_in_bze: false, module_swaps_held: false }))).toEqual([
      { key: "ok", tone: "ok", text: "Within limits." },
    ]);
  });

  it("handles a null average: no flags, a plain note", () => {
    const lines = poolStatus(pool({ avg_price: null, drift_pct: null, fees_in_bze: null, module_swaps_held: null }));
    expect(lines.map((l) => l.key)).toEqual(["unknown"]);
  });

  it("has no rule for a pool without BZE", () => {
    expect(poolStatus(pool({ fees_in_bze: null, module_swaps_held: null }))).toEqual([]);
  });

  it("does not claim a percentage when the drift is unknown", () => {
    const [fees] = poolStatus(pool({ drift_pct: null }));
    expect(fees.text).toContain("not comparable with its recent average");
  });
});

describe("pool price helpers", () => {
  it("names the token that is not BZE, on either side", () => {
    expect(poolTicker(pool())).toBe("HONEY");
    const flipped = pool({ reserves: { base: asset("ubze", "BZE"), quote: asset("uhoney", "HONEY") } });
    expect(poolTicker(flipped)).toBe("HONEY");
    expect(poolTicker(pool({ reserves: { base: asset("uhoney", null), quote: asset("ubze", "BZE") } }))).toBe("uhoney");
  });

  it("prefers the display price and falls back to the chain one", () => {
    expect(poolPriceText(pool(), "price")).toBe("2");
    expect(poolPriceText(pool({ price: { chain: "2.0", display: null } }), "price")).toBe("2.0");
    expect(poolPriceText(pool({ avg_price: null }), "avg_price")).toBeNull();
    expect(poolPriceUnit(pool())).toBe("BZE per HONEY");
  });
});
