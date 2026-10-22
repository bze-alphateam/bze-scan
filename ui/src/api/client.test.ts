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
    await api.blocks({ before: 101, limit: 25 });
    await api.block(5);
    await api.blockEvents(5, { cursor: "abc" });
    await api.blockTransfers(5, { limit: 100, cursor: "t" });
    await api.transaction("ABCD");
    await api.validators({ status: "all" });
    await api.hallOfFame();
    await api.validator("bzevaloper1x");
    await api.validatorBlocks("bzevaloper1x", { limit: 5 });
    await api.stakeChanges();
    await api.validatorMovements("bzevaloper1x", { page: 2, limit: 10 });
    await api.search("thamar node");
    await api.accountActivity("bze1x", { category: "dex", cursor: "a", limit: 25 });
    await api.accountIbc("bze1x", { limit: 100 });
    await api.accountOpenOrders("bze1x");
    await api.tokens({ kind: "ibc", limit: 100 });
    await api.token("factory/bze1x/uhoney");
    await api.tokenEvents("ubze", { cursor: "c" });
    await api.tokenHolders("ubze", { limit: 10 });
    await api.tokenTransfers("ubze", {});
    await api.proposals({ status: "voting_period", limit: 20 });
    await api.proposal(48);
    await api.proposalVotes(48, { voter: "validators", limit: 100 });
    await api.proposalVotes(48, { option: "no_with_veto", cursor: "v" });
    await api.proposalDeposits(48, { limit: 100 });
    await api.proposalNonVoters(45, { limit: 100, cursor: "n" });
    await api.stats();
    await api.params();
    await api.rawBlock(7);
    await api.rawBlockResults(7);
    await api.rawCommit(7);
    await api.rawTx("ABCD");
    expect(urls.map((u) => u.replace("http://h/api", ""))).toEqual([
      "/v1/status",
      "/v1/blocks",
      "/v1/blocks?limit=25&before=101",
      "/v1/blocks/5",
      "/v1/blocks/5/events?cursor=abc",
      "/v1/blocks/5/transfers?cursor=t&limit=100",
      "/v1/txs/ABCD",
      "/v1/validators?status=all",
      "/v1/validators/hall-of-fame",
      "/v1/validators/bzevaloper1x",
      "/v1/validators/bzevaloper1x/blocks?limit=5",
      "/v1/validators/stake-changes",
      "/v1/validators/bzevaloper1x/movements?page=2&limit=10",
      "/v1/search?q=thamar+node",
      "/v1/accounts/bze1x/activity?cursor=a&limit=25&category=dex",
      "/v1/accounts/bze1x/ibc?limit=100",
      "/v1/accounts/bze1x/open-orders",
      "/v1/tokens?limit=100&kind=ibc",
      "/v1/tokens/factory%2Fbze1x%2Fuhoney",
      "/v1/tokens/ubze/events?cursor=c",
      "/v1/tokens/ubze/holders?limit=10",
      "/v1/tokens/ubze/transfers",
      "/v1/proposals?limit=20&status=voting_period",
      "/v1/proposals/48",
      "/v1/proposals/48/votes?limit=100&voter=validators",
      "/v1/proposals/48/votes?cursor=v&option=no_with_veto",
      "/v1/proposals/48/deposits?limit=100",
      "/v1/proposals/45/non-voters?cursor=n&limit=100",
      "/v1/stats",
      "/v1/params",
      "/v1/raw/block/7",
      "/v1/raw/block_results/7",
      "/v1/raw/commit/7",
      "/v1/raw/tx/ABCD",
    ]);
  });

  it("asks for a denom with a slash by ?denom= when the path form answers 404", async () => {
    const urls: string[] = [];
    const fetch = vi.fn<typeof globalThis.fetch>(async (url) => {
      urls.push(String(url).replace("http://h/api", ""));
      return String(url).includes("/tokens/")
        ? json(404, { error: { code: "not_found", message: "no route" } })
        : json(200, { items: [], next_cursor: null });
    });
    const api = createHttpApi({ baseUrl: "http://h/api", fetch });
    await api.tokenHolders("factory/bze1x/uhoney", { limit: 10 });
    expect(urls).toEqual([
      "/v1/tokens/factory%2Fbze1x%2Fuhoney/holders?limit=10",
      "/v1/token/holders?denom=factory%2Fbze1x%2Fuhoney&limit=10",
    ]);
    // A denom without a slash has no other form: its 404 stands.
    await expect(api.token("uzzz")).rejects.toMatchObject({ status: 404 });
    expect(urls).toHaveLength(3);
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
