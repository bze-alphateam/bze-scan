import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { Page, Token, TokenEvent, TokenHolder, TokenSummary, TokenTransfer } from "@/api/types";
import { appIconsIn, fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import honeyJson from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney.json";
import holdersJson from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney/holders.json";
import transfersJson from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney/transfers.json";
import list from "../../../test/fixtures/api/v1/tokens.json";
import usdcJson from "../../../test/fixtures/api/v1/tokens/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4.json";
import atoneJson from "../../../test/fixtures/api/v1/tokens/ibc/E7CF15949EE673E0BD3F5AA7D711DD0A6C22992E7E5714A174DCEA289EB696A1.json";

import { TokenPage } from "./token-page";

const honey = honeyJson as Token;
const usdc = usdcJson as Token;
const atone = atoneJson as Token;
const holders = holdersJson as Page<TokenHolder>;
const transfers = transfersJson as Page<TokenTransfer>;
const empty = { items: [], next_cursor: null };
const texts = (testId: string) => screen.getAllByTestId(testId).map((e) => e.textContent);

function api(token: Token = honey, extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    token: () => Promise.resolve(token),
    tokenHolders: () => Promise.resolve(token === honey ? holders : { ...holders, items: [], next_cursor: null }),
    tokenTransfers: () => Promise.resolve(token === honey ? transfers : empty),
    ...extra,
  });
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"], now: new Date("2099-10-11T10:00:00Z") });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("TokenPage", () => {
  it("shows the header, tiles, about card, holder and activity skeletons on the first load", () => {
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: pendingApi() });
    expect(screen.getByTestId("token-skeleton")).toBeInTheDocument();
    expect(screen.getAllByTestId("tile-skeleton")).toHaveLength(4);
    expect(screen.getByTestId("card-skeleton")).toBeInTheDocument();
    expect(screen.getAllByTestId("list-skeleton-row")).toHaveLength(10);
  });

  it("draws a one-hop token's way from its home chain, without the multi-hop callout", async () => {
    renderWithProviders(<TokenPage denom={usdc.denom} />, { api: api(usdc) });
    const route = await screen.findByTestId("ibc-route");
    expect(route).toHaveTextContent("How it got heredirect · 1 hop");
    expect(screen.getAllByTestId("route-stop").map((s) => s.getAttribute("data-role"))).toEqual(["origin", "here"]);
    expect(texts("route-stop")).toEqual(["Nobleminted hereuusdc", "BZEthis tokenUSDC"]);
    expect(texts("route-link")).toEqual(["channel-1→↓on BZE · open"]);
    expect(screen.queryByTestId("hops-callout")).not.toBeInTheDocument();
  });

  it("tells a two-hop token apart from the direct one and draws the chain it came through", async () => {
    const directAtone = { ...atone, denom: "ibc/DIRECTATONE", ibc_hops: 1, via_chain: null };
    const tokens = () => Promise.resolve({ items: [atone, directAtone], next_cursor: null } as Page<TokenSummary>);
    renderWithProviders(<TokenPage denom={atone.denom} />, { api: api(atone, { tokens }) });
    const callout = await screen.findByTestId("hops-callout");
    expect(callout).toHaveAttribute("data-tone", "warn");
    expect(callout).toHaveTextContent(
      "Not the same token as ATONE sent straight from AtomOneIt came through Osmosis, so BZE sees it as a different token from the ATONE that came directly: the two are not interchangeable on BZE. " +
        "To turn it into the direct one, send it back to Osmosis and on to AtomOne, then over to BZE.",
    );
    expect(await within(callout).findByRole("link", { name: "See ATONE sent directly from AtomOne →" })).toHaveAttribute(
      "href",
      "/tokens/ibc%2FDIRECTATONE",
    );
    expect(screen.getByTestId("token-chips")).toHaveTextContent(/^Cross-chainfrom AtomOnevia Osmosis2 hops$/);
    expect(screen.getByTestId("ibc-route")).toHaveTextContent("2 hops · not direct");
    expect(texts("route-stop")).toEqual(["AtomOneminted hereuatone", "Osmosispassed through", "BZEthis tokenATONE · via Osmosis"]);
    expect(texts("route-link")).toEqual(["channel-94814→↓on Osmosis", "channel-0→↓on BZE · open"]);
    const about = screen.getByTestId("token-about");
    expect(about).toHaveTextContent("Minted onAtomOneas uatone");
    expect(about).toHaveTextContent("Came throughOsmosis· 2 hops, not direct");
    expect(texts("ibc-path")).toEqual(["transfer/channel-0 · BZE to Osmosis", "transfer/channel-94814 · Osmosis to AtomOne"]);
    expect(about).toHaveTextContent("Path on BZE");
    expect(about).toHaveTextContent("Path on Osmosis");
    expect(about).not.toHaveTextContent("Origin chain");
    expect(screen.getByText(/minted on AtomOne and reached BZE through Osmosis\./)).toBeInTheDocument();
    expect(await screen.findByText("No activity indexed yet.")).toBeInTheDocument();
  });

  it("counts the chains between on a three-hop route instead of naming them", async () => {
    const three = { ...atone, ibc_hops: 3, ibc_path: "transfer/channel-0/transfer/channel-94814/transfer/channel-7" };
    renderWithProviders(<TokenPage denom={atone.denom} />, { api: api(three) });
    await screen.findByTestId("ibc-route");
    expect(screen.getAllByTestId("route-stop").map((s) => [s.getAttribute("data-role"), s.firstChild?.textContent])).toEqual([
      ["origin", "AtomOne"],
      ["counted", "1 more chain"],
      ["through", "Osmosis"],
      ["here", "BZE"],
    ]);
    expect(texts("route-link")).toEqual(["channel-7→↓on another chain", "channel-94814→↓on Osmosis", "channel-0→↓on BZE · open"]);
    expect(screen.getByTestId("hops-callout")).toBeInTheDocument();
    expect(texts("ibc-path")).toHaveLength(3);
  });

  it("names a pool share's two assets", async () => {
    const lp = {
      ...usdc,
      denom: "ulp/7E2E1C1A9E41C35A4F3B0CBA6A5F1C0E8D9B6A1F4C3E2D1B0A9F8E7D6C5B4A39",
      symbol: "BZE/USDC LP",
      kind: "lp",
      origin_chain: null,
      ibc_hops: null,
      lp_pair: { base: "ubze", quote: usdc.denom },
    };
    renderWithProviders(<TokenPage denom={lp.denom} />, {
      api: api(lp, { tokens: () => Promise.resolve(list as Page<TokenSummary>) }),
    });
    const pair = await screen.findByTestId("pool-pair");
    expect(screen.queryByTestId("hops-callout")).not.toBeInTheDocument();
    expect(screen.queryByTestId("ibc-route")).not.toBeInTheDocument();
    await within(pair).findByRole("link", { name: "BZE" });
    expect(pair).toHaveTextContent("A share of the DEX pool of BZE and USDC.");
    expect(within(pair).getByRole("link", { name: "USDC" })).toHaveAttribute("href", `/tokens/${encodeURIComponent(usdc.denom)}`);
    expect(screen.getByTestId("lp-logo")).toBeInTheDocument();
  });

  it("renders a Factory token whose admin can change the supply", async () => {
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api() });
    expect(await screen.findByRole("heading", { level: 1, name: "HONEY" })).toBeInTheDocument();
    expect(screen.getByTestId("token-chips")).toHaveTextContent("FactoryTrading on the DEXhoney.example");
    expect(screen.getByTestId("token-created")).toHaveTextContent("Created Oct 2, 2099 by3Gbze13gz…yhqk");
    expect(screen.getByRole("link", { name: /View on the DEX/ })).toHaveAttribute("href", expect.stringContaining("uhoney"));
    expect(appIconsIn(screen.getByRole("link", { name: /View on the DEX/ }))).toEqual(["dex"]);
    expect(screen.getAllByTestId("tile").map((t) => t.textContent)).toEqual([
      "Total supply500K HONEYcan change · see below",
      "Holders310accounts with at least 1 HONEY",
      "Price$0.002on the DEX",
      "Moved in 24h125K HONEY14 transfers",
    ]);

    const box = screen.getByTestId("supply-box");
    expect(box).toHaveAttribute("data-state", "admin");
    expect(box).toHaveAttribute("data-tone", "warn");
    expect(box).toHaveTextContent(
      "The supply of HONEY can changeThe token's admin, 3Gbze13gz…yhqk, can create more HONEY or destroy some at any time. So far: 2 mints adding 1,000,500 HONEY, 1 burn removing 200 HONEY.",
    );

    const about = screen.getByTestId("token-about");
    expect(about).toHaveTextContent("NameHoney");
    expect(about).toHaveTextContent("CreatedOct 2, 2099·see transaction");
    expect(about).toHaveTextContent("Admin3Gbze13gz…yhqksame account · can mint, burn and change the branding");
    expect(about).toHaveTextContent("Decimals6");
    expect(about).toHaveTextContent("DescriptionThe sweetest token on BZE.");
    expect(within(about).getByRole("link", { name: "see transaction" })).toHaveAttribute(
      "href",
      "/txs/5A1B2C3D4E5F60718293A4B5C6D7E8F90A1B2C3D4E5F60718293A4B5C6D7E8F9",
    );
    // The raw denom waits in More details.
    expect(screen.queryByTestId("token-denom")).not.toBeInTheDocument();
  });

  it("lists the top holders and loads them all on request", async () => {
    const tokenHolders = vi.fn((_denom: string, p: { cursor?: string }) =>
      Promise.resolve(
        p.cursor
          ? { items: [{ rank: 7, address: "bze1tail", label: null, balance: "1000000", share_pct: "0.00020" }], next_cursor: null }
          : holders,
      ),
    );
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api(honey, { tokenHolders }) });
    const rows = await screen.findAllByTestId("holder-row");
    expect(rows).toHaveLength(6);
    expect(screen.getByText("the top 5 hold 87.5%")).toBeInTheDocument();
    expect(rows[0]).toHaveTextContent("1DEDEX250K HONEY50.0%");
    expect(rows[4]).toHaveTextContent("BZE team");
    await userEvent.click(screen.getByRole("button", { name: "Show all 310 holders" }));
    expect(await screen.findAllByTestId("holder-row")).toHaveLength(7);
    expect(tokenHolders.mock.calls.map((c) => c[1])).toEqual([{ limit: 10 }, { cursor: holders.next_cursor, limit: 100 }]);
    expect(screen.queryByRole("button", { name: /Show/ })).not.toBeInTheDocument();
  });

  it("does not offer all N holders when N, the holders of at least 1 token, is fewer than the rows shown", async () => {
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api({ ...honey, holders_count: 3 }, { tokenHolders: () => Promise.resolve(holders) }) });
    expect(await screen.findAllByTestId("holder-row")).toHaveLength(6);
    expect(screen.getByRole("button", { name: "Show more holders" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Show all/ })).not.toBeInTheDocument();
  });

  it("merges the events with the transfers, newest first", async () => {
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api() });
    await screen.findAllByTestId("holder-row");
    const items = await screen.findAllByTestId("activity-item");
    expect(items.map((i) => i.firstChild?.textContent)).toEqual([
      "SentM3bze1m3k…9ds4→25,000 HONEY→Q8bze1q8f…x0rq",
      "Traded 75,000 HONEY on the DEXbyM3bze1m3k…9ds4",
      "Market created on the DEX",
      "Burned 200 HONEY by the admin",
      "Minted 500 HONEY by the admin",
      "Created by3Gbze13gz…yhqk",
      "Minted 1,000,000 HONEY by the admin",
    ]);
    expect(within(items[0]).getByRole("link", { name: "see transaction" })).toHaveAttribute(
      "href",
      expect.stringMatching(/^\/txs\/9E1B/),
    );
    expect(within(items[2]).queryByRole("link", { name: "see transaction" })).not.toBeInTheDocument();
  });

  it("pages older transfers and keeps older events behind them", async () => {
    const tokenTransfers = vi.fn((_denom: string, p: { cursor?: string }) =>
      Promise.resolve(
        p.cursor
          ? { items: transfers.items.slice(1), next_cursor: null }
          : { items: transfers.items.slice(0, 1), next_cursor: "t1" },
      ),
    );
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api(honey, { tokenTransfers }) });
    expect(await screen.findAllByTestId("activity-item")).toHaveLength(1);
    await userEvent.click(screen.getByRole("button", { name: "Show older activity" }));
    expect(await screen.findAllByTestId("activity-item")).toHaveLength(7);
    expect(tokenTransfers.mock.calls[1][1]).toEqual({ cursor: "t1", limit: 20 });
  });

  it("reads every older event before totalling the mints and burns", async () => {
    const older: TokenEvent = { ...honey.events[2], height: 23000000, amount: "1000000000" };
    const tokenEvents = vi.fn(() => Promise.resolve({ items: [older], next_cursor: null }));
    renderWithProviders(<TokenPage denom={honey.denom} />, {
      api: api({ ...honey, events_next_cursor: "e1" }, { tokenEvents }),
    });
    expect(await screen.findByText(/So far: 3 mints adding 1,001,500 HONEY/)).toBeInTheDocument();
    expect(tokenEvents).toHaveBeenCalledWith(honey.denom, { limit: 100, cursor: "e1" }, expect.anything());
  });

  it("says a renounced Factory token's supply is fixed", async () => {
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api({ ...honey, admin: null }) });
    const box = await screen.findByTestId("supply-box");
    expect(box).toHaveAttribute("data-state", "fixed");
    expect(box).toHaveTextContent("The supply of HONEY is fixed");
    expect(screen.getAllByTestId("tile")[0]).toHaveTextContent("Total supply500K HONEYfixed");
    expect(screen.getByTestId("token-about")).toHaveTextContent("AdminRenouncednobody can mint or burn");
    expect(box).toHaveAttribute("data-tone", "ok");
  });

  it("renders a cross-chain token with its origin chain", async () => {
    renderWithProviders(<TokenPage denom={usdc.denom} />, { api: api(usdc) });
    expect(await screen.findByRole("heading", { level: 1, name: "USDC" })).toBeInTheDocument();
    expect(screen.getByTestId("token-chips")).toHaveTextContent("Cross-chainNobleTrading on the DEX");
    expect(screen.queryByTestId("token-created")).not.toBeInTheDocument();
    expect(screen.queryByTestId("stranded-explainer")).not.toBeInTheDocument();
    const box = screen.getByTestId("supply-box");
    expect(box).toHaveAttribute("data-state", "elsewhere");
    expect(box).toHaveTextContent("USDC is minted on Noble");
    const about = screen.getByTestId("token-about");
    expect(about).toHaveTextContent("Origin chainNoble");
    expect(about).toHaveTextContent("Base denom thereuusdc");
    expect(about).toHaveTextContent("Pathtransfer/channel-1");
    expect(within(about).getByTestId("ibc-connection")).toHaveTextContent("channel-1Openand active");
    expect(within(about).getByTestId("ibc-path")).toHaveTextContent("transfer/channel-1 on BZE, to Noble");
    expect(about).not.toHaveTextContent("Admin");
    expect(await screen.findByText("No activity indexed yet.")).toBeInTheDocument();
  });

  it("warns about a stranded token and says why, its chain shut down and its connection expired", async () => {
    const stranded = {
      ...usdc,
      origin_chain: { chain_id: "noble-1", name: "Noble", logo_url: null, status: "killed" },
      ibc_channel: { channel_id: "channel-1", state: "open", client_status: "expired" },
      stranded: true,
    };
    renderWithProviders(<TokenPage denom={usdc.denom} />, { api: api(stranded) });
    expect(await screen.findByTestId("stranded-tag")).toHaveTextContent("Stranded");
    expect(screen.getByTestId("token-chips")).toHaveTextContent("Cross-chainNoble (shut down)Stranded");
    expect(screen.getByTestId("stranded-explainer")).toHaveTextContent(
      "Noble, where it was minted, has shut down. BZE's connection to Noble (channel-1) has expired",
    );
    const about = screen.getByTestId("token-about");
    expect(within(about).getByTestId("chain-dead")).toHaveTextContent("shut down");
    expect(within(about).getByTestId("ibc-connection")).toHaveTextContent("channel-1Expiredno relayer kept it updated");
    expect(screen.getByTestId("route-link")).toHaveTextContent("on BZE · expired");
    // Every badge explains itself on tap, not only on hover.
    await userEvent.click(screen.getByTestId("stranded-tag"));
    expect(await screen.findByRole("dialog")).toHaveTextContent("can never be sent back");
  });

  it("keeps the full name, base unit and raw metadata in More details", async () => {
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api() });
    await userEvent.click(await screen.findByRole("button", { name: /More details/ }));
    expect(screen.getByTestId("token-denom")).toHaveTextContent(honey.denom);
    expect(screen.getByText("1 HONEY = 1,000,000 uhoney")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy JSON" })).toBeInTheDocument();
    // On the raw bar from c2 up, under the JSON on phones.
    expect(screen.getAllByRole("link", { name: /Open on Ping.pub/ })).toHaveLength(2);
  });

  it("says inside its own panel when the holders fail, and retries them", async () => {
    const tokenHolders = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api(honey, { tokenHolders }) });
    const error = await screen.findByTestId("section-error");
    expect(error).toHaveTextContent("The holders could not be loaded.Try again");
    // The rest of the page stays.
    expect(screen.getAllByTestId("activity-item")).toHaveLength(7);
    const calls = tokenHolders.mock.calls.length;
    await userEvent.click(within(error).getByRole("button", { name: "Try again" }));
    expect(tokenHolders.mock.calls.length).toBeGreaterThan(calls);
  });

  it("keeps the events when the transfers fail and offers to retry them", async () => {
    const tokenTransfers = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api(honey, { tokenTransfers }) });
    expect(await screen.findByTestId("section-error")).toHaveTextContent("The transfers could not be loaded.");
    expect(screen.getAllByTestId("activity-item").length).toBeGreaterThan(0);
    expect(screen.queryByText("No activity indexed yet.")).not.toBeInTheDocument();
  });

  it("answers not found for a denom the API does not know", async () => {
    const token = vi.fn(() => Promise.reject(new ApiError(404, "not_found", "unknown denom")));
    renderWithProviders(<TokenPage denom="factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uzzz" />, { api: api(honey, { token }) });
    expect(await screen.findByRole("heading", { name: "Token not found" })).toBeInTheDocument();
    expect(screen.getByTestId("token-not_found")).toHaveAttribute("data-state", "neutral");
    expect(screen.getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Tokens/factory/bze1…/uzzz");
    expect(screen.getByRole("heading", { level: 1, name: "Token" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "tokens list" })).toHaveAttribute("href", "/tokens");
  });

  it("offers a retry when the API fails", async () => {
    const token = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api(honey, { token }) });
    expect(await screen.findByTestId("token-error")).toHaveAttribute("data-state", "error");
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(token).toHaveBeenCalledTimes(2);
  });
});
