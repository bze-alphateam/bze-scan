import { describe, expect, it, vi } from "vitest";

import { SERVER_FETCH_TIMEOUT_MS, serverJson } from "./server-json";

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("serverJson", () => {
  it("returns the parsed body of a 2xx and reads it to the end", async () => {
    const res = json({ hash: "AB" });
    const result = await serverJson<{ hash: string }>("/v1/txs/AB", async () => res);
    expect(result).toEqual({ ok: true, data: { hash: "AB" } });
    expect(res.bodyUsed).toBe(true);
  });

  it("asks the server-side API URL with no-store, JSON and a timeout", async () => {
    const fetchFn = vi.fn(async () => json({}));
    await serverJson("/v1/proposals/7", fetchFn);
    const [url, init] = fetchFn.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toMatch(/\/v1\/proposals\/7$/);
    expect(init.cache).toBe("no-store");
    expect(init.headers).toEqual({ Accept: "application/json" });
    expect(init.signal).toBeInstanceOf(AbortSignal);
    expect(SERVER_FETCH_TIMEOUT_MS).toBeGreaterThan(0);
  });

  // Regression: a 404 (unknown account, pending transaction) left its body
  // unread, holding the connection and its buffers until garbage collection.
  it.each([404, 400, 500])("cancels the unread body of a %i and returns the status", async (status) => {
    const res = json({ error: "nope" }, status);
    const result = await serverJson("/v1/accounts/bze1x", async () => res);
    expect(result).toEqual({ ok: false, status });
    await expect(res.text()).rejects.toThrow();
  });

  it("returns a null status when the request fails or times out", async () => {
    const result = await serverJson("/v1/tokens/ubze", async () => {
      throw new DOMException("timed out", "TimeoutError");
    });
    expect(result).toEqual({ ok: false, status: null });
  });

  it("returns a null status for a 2xx that is not JSON", async () => {
    const result = await serverJson("/v1/tokens/ubze", async () => new Response("<html>", { status: 200 }));
    expect(result).toEqual({ ok: false, status: null });
  });
});
