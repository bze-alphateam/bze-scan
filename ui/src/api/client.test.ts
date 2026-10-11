import { describe, expect, it, vi } from "vitest";

import { ApiError, createHttpApi } from "./client";

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function apiWith(response: Response) {
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(response);
  return { fetch, api: createHttpApi({ baseUrl: "https://scan.example/api/", fetch }) };
}

describe("createHttpApi", () => {
  it("builds /v1 URLs and drops empty query values", async () => {
    const { fetch, api } = apiWith(json(200, { items: [], next_cursor: null }));
    const signal = new AbortController().signal;
    await api.transactions({ limit: 10, cursor: undefined, status: "failed" }, signal);
    expect(fetch).toHaveBeenCalledWith("https://scan.example/api/v1/txs?limit=10&status=failed", {
      headers: { Accept: "application/json" },
      signal,
    });
  });

  it("encodes path values", async () => {
    const { fetch, api } = apiWith(json(200, {}));
    await api.account("bze1a/../b?c");
    expect(fetch.mock.calls[0][0]).toBe("https://scan.example/api/v1/accounts/bze1a%2F..%2Fb%3Fc");
  });

  it("maps every route", async () => {
    const urls: string[] = [];
    const fetch = vi.fn<typeof globalThis.fetch>(async (url) => {
      urls.push(String(url));
      return json(200, {});
    });
    const api = createHttpApi({ baseUrl: "http://h/api", fetch });
    await api.status();
    await api.blocks({});
    await api.block(5);
    await api.blockEvents(5, { cursor: "abc" });
    await api.transaction("ABCD");
    await api.validators({ status: "all" });
    await api.validator("bzevaloper1x");
    await api.validatorBlocks("bzevaloper1x", { limit: 5 });
    await api.search("thamar node");
    await api.rawBlock(7);
    await api.rawBlockResults(7);
    await api.rawCommit(7);
    await api.rawTx("ABCD");
    expect(urls.map((u) => u.replace("http://h/api", ""))).toEqual([
      "/v1/status",
      "/v1/blocks",
      "/v1/blocks/5",
      "/v1/blocks/5/events?cursor=abc",
      "/v1/txs/ABCD",
      "/v1/validators?status=all",
      "/v1/validators/bzevaloper1x",
      "/v1/validators/bzevaloper1x/blocks?limit=5",
      "/v1/search?q=thamar+node",
      "/v1/raw/block/7",
      "/v1/raw/block_results/7",
      "/v1/raw/commit/7",
      "/v1/raw/tx/ABCD",
    ]);
  });

  it("turns the error envelope into an ApiError", async () => {
    const { api } = apiWith(json(404, { error: { code: "not_found", message: "block 9 is not indexed" } }));
    const err = await api.block(9).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 404, code: "not_found", message: "block 9 is not indexed", isClientError: true });
  });

  it("keeps the status of an answer without an envelope", async () => {
    const { api } = apiWith(new Response("<html>Bad Gateway</html>", { status: 502, statusText: "Bad Gateway" }));
    const err = await api.status().catch((e: unknown) => e);
    expect(err).toMatchObject({ status: 502, code: "http_error", message: "Bad Gateway", isClientError: false });
  });

  it("lets network errors through", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>().mockRejectedValue(new TypeError("Failed to fetch"));
    const api = createHttpApi({ baseUrl: "http://h/api", fetch });
    await expect(api.status()).rejects.toThrow(TypeError);
  });
});
