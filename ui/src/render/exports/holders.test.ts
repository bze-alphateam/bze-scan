import { describe, expect, it, vi, type Mock } from "vitest";

import type { Page, Token, TokenHolder } from "@/api/types";
import { runExport, type FilterValues } from "@/lib/export";
import { fakeApi } from "@/test/render";

import token from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney.json";
import holders from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney/holders.json";

import { holdersExport } from "./holders";

const t = token as unknown as Token;
const items = (holders as Page<TokenHolder>).items;
const escrow: TokenHolder = {
  rank: 7,
  address: "bze1escrowescrowescrowescrowescrowescrow0",
  label: { name: "IBC escrow channel-0", kind: "ibc_escrow" },
  balance: "1000000000",
  share_pct: "0.20000",
};
const all = [...items, escrow];

type ReadHolders = Mock<(denom: string, params: object) => Promise<Page<TokenHolder>>>;

async function exported(filters: FilterValues, tokenHolders: ReadHolders = vi.fn(async () => ({ items: all, next_cursor: null }))) {
  const def = holdersExport({ api: fakeApi({ tokenHolders }), token: t });
  const done = await runExport(def.source(filters), { limit: 20_000, limiter: { wait: async () => {} }, signal: new AbortController().signal });
  return { def, done, tokenHolders };
}

describe("holdersExport", () => {
  it("leaves out module accounts and IBC escrows by default", async () => {
    const { def, done } = await exported(holdersExport({ api: fakeApi(), token: t }).initialFilters);
    expect(def.initialFilters).toEqual({ minBalance: "", excludeModules: true });
    expect(done.items.map((h) => h.rank)).toEqual([2, 3, 4, 5, 6]);
  });

  it("keeps them when the box is unticked", async () => {
    const { done } = await exported({ minBalance: "", excludeModules: false });
    expect(done.items).toHaveLength(7);
  });

  it("ends the read at the first balance under the minimum, given in whole tokens", async () => {
    const tokenHolders: ReadHolders = vi.fn(async () => ({ items: all, next_cursor: "more" }));
    const { done } = await exported({ minBalance: "12500", excludeModules: false }, tokenHolders);
    expect(done.items.map((h) => h.rank)).toEqual([1, 2, 3, 4, 5]);
    expect(done.finished).toBe(true);
    // The rest is sorted under the minimum: no second read.
    expect(tokenHolders).toHaveBeenCalledTimes(1);
  });

  it("reads 100 a page by cursor", async () => {
    const tokenHolders: ReadHolders = vi
      .fn()
      .mockResolvedValueOnce({ items: items.slice(0, 3), next_cursor: "c1" })
      .mockResolvedValueOnce({ items: items.slice(3), next_cursor: null });
    await exported({ minBalance: "", excludeModules: true }, tokenHolders);
    expect(tokenHolders.mock.calls.map((c) => c[1])).toEqual([
      { cursor: undefined, limit: 100 },
      { cursor: "c1", limit: 100 },
    ]);
  });

  it("refuses a minimum that is not a number, or is negative", () => {
    const def = holdersExport({ api: fakeApi(), token: t });
    expect(def.validate!({ minBalance: "", excludeModules: true })).toBeNull();
    expect(def.validate!({ minBalance: "0.5", excludeModules: true })).toBeNull();
    expect(def.validate!({ minBalance: "abc", excludeModules: true })).toMatch(/must be a number/);
    expect(def.validate!({ minBalance: "-1", excludeModules: true })).toMatch(/must be a number/);
  });

  it("writes rank, address, name, balance in whole tokens, symbol and share", () => {
    const def = holdersExport({ api: fakeApi(), token: t });
    expect(def.fields.map((f) => f.header)).toEqual(["Rank", "Address", "Name", "Balance", "Symbol", "Share of supply (%)"]);
    expect(def.fields.map((f) => f.value(items[4]))).toEqual([5, items[4].address, "BZE team", "12500", "HONEY", "2.50000"]);
    expect(def.limit).toEqual({ max: 20_000, initial: 20_000 });
    expect(def.fileName(new Date("2026-10-29T14:22:33Z"))).toBe("holders-HONEY-20261029-142233.csv");
  });
});
