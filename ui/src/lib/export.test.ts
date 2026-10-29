import { describe, expect, it, vi } from "vitest";

import { progressShare, runExport, startProgress, type ExportPage, type ExportProgress, type ExportSource } from "./export";
import type { RateLimiter } from "./rate-limit";

/** A list of numbers 1…n served `size` a page, the cursor being the next index. */
function numbers(n: number, size: number, extra: Partial<ExportSource<number>> = {}) {
  const fetchPage = vi.fn(async (cursor: string | null): Promise<ExportPage<number>> => {
    const from = cursor == null ? 0 : Number(cursor);
    const items = Array.from({ length: Math.max(0, Math.min(size, n - from)) }, (_, i) => from + i + 1);
    return { items, next: from + size < n ? String(from + size) : null, total: n };
  });
  return { ...extra, fetchPage } satisfies ExportSource<number>;
}

const limiter = (): RateLimiter & { calls: number } => {
  const l = { calls: 0, wait: async () => void l.calls++ };
  return l;
};

const signal = () => new AbortController().signal;

describe("runExport", () => {
  it("reads every page to the end, one limiter wait per read", async () => {
    const source = numbers(250, 100);
    const pace = limiter();
    const done = await runExport(source, { limit: 10_000, limiter: pace, signal: signal() });
    expect(done.items).toHaveLength(250);
    expect(done.items.at(-1)).toBe(250);
    expect(done).toMatchObject({ scanned: 250, total: 250, finished: true });
    expect(source.fetchPage).toHaveBeenCalledTimes(3);
    expect(pace.calls).toBe(3);
  });

  it("stops at the limit, mid-page", async () => {
    const source = numbers(1_000, 100);
    const done = await runExport(source, { limit: 150, limiter: limiter(), signal: signal() });
    expect(done.items).toHaveLength(150);
    expect(source.fetchPage).toHaveBeenCalledTimes(2);
  });

  it("drops what keep refuses, counting only kept items against the limit", async () => {
    const source = numbers(1_000, 100, { keep: (x) => x % 2 === 0 });
    const done = await runExport(source, { limit: 120, limiter: limiter(), signal: signal() });
    expect(done.items).toHaveLength(120);
    expect(done.items.every((x) => x % 2 === 0)).toBe(true);
    expect(done.scanned).toBe(240);
  });

  it("ends at the first item stop names, leaving it out", async () => {
    const source = numbers(1_000, 100, { stop: (x) => x > 130 });
    const done = await runExport(source, { limit: 10_000, limiter: limiter(), signal: signal() });
    expect(done.items).toHaveLength(130);
    expect(done.finished).toBe(true);
    expect(source.fetchPage).toHaveBeenCalledTimes(2);
  });

  it("ends when the cursor does not move, rather than reading the same page forever", async () => {
    const fetchPage = vi.fn(async () => ({ items: [1, 2], next: "same" }));
    const done = await runExport({ fetchPage }, { limit: 10_000, limiter: limiter(), signal: signal() });
    expect(fetchPage).toHaveBeenCalledTimes(2);
    expect(done.items).toEqual([1, 2, 1, 2]);
  });

  it("reports every page and, after a failure, carries on from the last report", async () => {
    const source = numbers(300, 100);
    source.fetchPage.mockImplementationOnce(source.fetchPage.getMockImplementation()!).mockRejectedValueOnce(new Error("502"));
    const reports: ExportProgress<number>[] = [];
    await expect(
      runExport(source, { limit: 10_000, limiter: limiter(), signal: signal(), onProgress: (p) => reports.push(p) }),
    ).rejects.toThrow("502");
    expect(reports).toHaveLength(1);
    expect(reports[0]).toMatchObject({ cursor: "100", finished: false });

    const done = await runExport(source, { limit: 10_000, limiter: limiter(), signal: signal(), from: reports[0] });
    expect(done.items).toEqual(Array.from({ length: 300 }, (_, i) => i + 1));
  });

  it("rejects when the limiter is aborted", async () => {
    const controller = new AbortController();
    controller.abort();
    const pace: RateLimiter = { wait: async (s) => (s?.aborted ? Promise.reject(new Error("aborted")) : undefined) };
    await expect(runExport(numbers(10, 5), { limit: 10, limiter: pace, signal: controller.signal })).rejects.toThrow("aborted");
  });
});

describe("progressShare", () => {
  it("is kept items against the limit, or read items against the list's size, never 1 before the end", () => {
    const p = { ...startProgress<number>(), items: [1, 2, 3], scanned: 3 };
    expect(progressShare(p, 10)).toBeCloseTo(0.3);
    expect(progressShare({ ...p, total: 6 }, 10)).toBeCloseTo(0.5);
    expect(progressShare({ ...p, scanned: 40 }, 10, 50)).toBeCloseTo(0.8);
    expect(progressShare({ ...p, items: Array(10).fill(0) }, 10)).toBe(0.99);
    expect(progressShare({ ...p, finished: true }, 10)).toBe(1);
  });
});
