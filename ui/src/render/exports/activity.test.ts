import { describe, expect, it, vi } from "vitest";

import type { Activity, ActivityPage, Page } from "@/api/types";
import { startProgress, runExport } from "@/lib/export";
import { toCsv } from "@/lib/csv";
import { createRenderContext } from "@/render/messages/registry";
import { fakeApi } from "@/test/render";

import activity from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/activity.json";

import { activityExport, activityLines } from "./activity";

const ADDR = "bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4";
const rows = (activity as Page<Activity>).items;
const ctx = createRenderContext({ transfers: [] });
const pace = { wait: async () => {} };

/** A swap that sold BZE for HONEY, the fee in BZE. */
const swap: Activity = {
  ...rows[0],
  kind: "dex_swap",
  category: "dex",
  counterparty: null,
  deltas: [
    { denom: "ubze", amount: "-100002000", symbol: "BZE", exponent: 6 },
    { denom: "factory/bze1x/uhoney", amount: "40000000", symbol: "HONEY", exponent: 6 },
  ],
  fee: [{ denom: "ubze", amount: "2000", symbol: "BZE", exponent: 6 }],
  msg_types: ["/bze.tradebin.MsgMultiSwap"],
};

const csvOf = (def: ReturnType<typeof activityExport>, items: Activity[]) =>
  toCsv(
    def.fields.map((f) => f.header),
    items.flatMap((r) => def.lines(r)).map((l) => def.fields.map((f) => f.value(l))),
  );

describe("activityLines", () => {
  it("writes the amount the action moved, the fee apart and signed in display units", () => {
    const [line] = activityLines(rows[0], ADDR, ctx);
    expect(line).toMatchObject({ amount: "-10", symbol: "BZE", denom: "ubze", fee: { amount: "0.002", symbol: "BZE" } });
    expect(line.description).toMatch(/^Sent to /);
  });

  it("writes one line per denom, the fee only on the first", () => {
    const lines = activityLines(swap, ADDR, ctx);
    expect(lines.map((l) => [l.amount, l.symbol, l.first, l.fee?.amount ?? null])).toEqual([
      ["-100", "BZE", true, "0.002"],
      ["40", "HONEY", false, null],
    ]);
  });

  it("writes one line without an amount for a row that moved nothing but the fee", () => {
    const feeOnly = rows.find((r) => r.kind === "fee_only")!;
    expect(activityLines(feeOnly, ADDR, ctx)).toEqual([
      expect.objectContaining({ amount: null, symbol: null, fee: { amount: "0.002", symbol: "BZE" } }),
    ]);
  });

  it("keeps base units and no symbol for a denom of unknown exponent", () => {
    const odd: Activity = { ...rows[2], deltas: [{ denom: "factory/bze1y/umystery", amount: "123", symbol: null, exponent: null }] };
    expect(activityLines(odd, ADDR, ctx)[0]).toMatchObject({ amount: "123", symbol: null, denom: "factory/bze1y/umystery" });
  });
});

describe("activityExport", () => {
  it("writes the fixture as CSV: a header, then a line per denom, with block-level rows hash-less", () => {
    const def = activityExport({ api: fakeApi(), address: ADDR, category: null });
    const csv = csvOf(def, rows.slice(0, 2));
    expect(csv.split("\r\n")[0]).toBe(
      "Time (UTC),Block,Transaction hash,Description,Category,Kind,Amount,Symbol,Denom,Fee,Fee symbol," +
        "Counterparty,Counterparty name,Success,Signed by this account,Message types",
    );
    const [, sent, fill] = csv.split("\r\n");
    expect(sent).toMatch(/^2026-10-12 09:58:00,25120440,1{8}/);
    expect(sent).toContain(",sent,send,-10,BZE,ubze,0.002,BZE,bze18uf09nx6");
    expect(fill).toMatch(/^2026-10-12 09:41:00,25120301,,/);
    expect(fill).toContain(",dex,dex_fill,2.56,USDC,");
  });

  it("starts from the tab's category, with block-level rows in", () => {
    expect(activityExport({ api: fakeApi(), address: ADDR, category: "dex" }).initialFilters).toEqual({ category: "dex", blockLevel: true });
    expect(activityExport({ api: fakeApi(), address: ADDR, category: null }).initialFilters).toEqual({ category: "", blockLevel: true });
  });

  it("reads a numbered first page (for the total), then by cursor, 100 a read, of the chosen category", async () => {
    const accountActivity = vi
      .fn<(a: string, p: object) => Promise<ActivityPage>>()
      .mockResolvedValueOnce({ items: rows, next_cursor: "c1", total: 16 })
      // A cursor read carries no total.
      .mockResolvedValueOnce({ items: rows, next_cursor: null } as unknown as ActivityPage);
    const def = activityExport({ api: fakeApi({ accountActivity }), address: ADDR, category: null });
    const done = await runExport(def.source({ category: "dex", blockLevel: true }), {
      limit: 10_000,
      limiter: pace,
      signal: new AbortController().signal,
    });
    expect(accountActivity.mock.calls.map((c) => c[1])).toEqual([
      { page: 1, limit: 100, category: "dex" },
      { cursor: "c1", limit: 100, category: "dex" },
    ]);
    expect(done.items).toHaveLength(16);
    expect(done.total).toBe(16);
  });

  it("leaves out rows without a transaction when asked", async () => {
    const accountActivity = async () => ({ items: rows, next_cursor: null, total: rows.length });
    const def = activityExport({ api: fakeApi({ accountActivity }), address: ADDR, category: null });
    const done = await runExport(def.source({ category: "", blockLevel: false }), {
      limit: 10_000,
      limiter: pace,
      signal: new AbortController().signal,
      from: startProgress(),
    });
    expect(done.items).toHaveLength(rows.length - 1);
    expect(done.items.every((r) => r.tx_hash != null)).toBe(true);
  });

  it("caps the limit at 10,000 and names the file after the account", () => {
    const def = activityExport({ api: fakeApi(), address: ADDR, category: null });
    expect(def.limit).toEqual({ max: 10_000, initial: 10_000 });
    expect(def.fileName(new Date("2026-10-29T14:22:33Z"))).toBe(`transactions-${ADDR}-20261029-142233.csv`);
  });
});
