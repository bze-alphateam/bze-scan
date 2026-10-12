import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { Page, Token, TokenEvent, TokenHolder, TokenSummary, TokenTransfer } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import honeyJson from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney.json";
import holdersJson from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney/holders.json";
import transfersJson from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney/transfers.json";
import list from "../../../test/fixtures/api/v1/tokens.json";
import usdcJson from "../../../test/fixtures/api/v1/tokens/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4.json";

import { TokenPage } from "./token-page";

const honey = honeyJson as Token;
const usdc = usdcJson as Token;
const holders = holdersJson as Page<TokenHolder>;
const transfers = transfersJson as Page<TokenTransfer>;
const empty = { items: [], next_cursor: null };

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

  it("explains a multi-hop token and names a pool share's two assets", async () => {
    const twoHop = {
      ...usdc,
      ibc_path: "transfer/channel-1/transfer/channel-750",
      ibc_hops: 2,
      via_chain: { chain_id: "osmosis-1", name: "Osmosis", logo_url: null },
    };
    const { unmount } = renderWithProviders(<TokenPage denom={usdc.denom} />, { api: api(twoHop) });
    expect(await screen.findByTestId("hops-explainer")).toHaveTextContent(
      "This USDC came to BZE through Osmosis (2 hops), not straight from Noble.",
    );
    expect(screen.getByTestId("token-chips")).toHaveTextContent("2 hops via Osmosis");
    const about = screen.getByTestId("token-about");
    expect(within(about).getByTestId("ibc-route")).toHaveTextContent("NobleOsmosisBZE");
    expect(within(about).getByTestId("ibc-path")).toHaveTextContent(
      "transfer/channel-1 on BZE, to Osmosistransfer/channel-750 on Osmosis, to Noble",
    );
    expect(about).toHaveTextContent("Origin chainNoble· 2 hops via Osmosis");
    expect(screen.getByText(/minted on Noble and reached BZE through Osmosis\./)).toBeInTheDocument();
    unmount();

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
    expect(screen.queryByTestId("hops-explainer")).not.toBeInTheDocument();
    await within(pair).findByRole("link", { name: "BZE" });
    expect(pair).toHaveTextContent("A share of the DEX pool of BZE and USDC.");
    expect(within(pair).getByRole("link", { name: "USDC" })).toHaveAttribute("href", `/tokens/${encodeURIComponent(usdc.denom)}`);
    expect(screen.getByTestId("lp-logo")).toBeInTheDocument();
  });

  it("renders a Factory token whose admin can change the supply", async () => {
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api() });
    expect(await screen.findByRole("heading", { level: 1, name: "HONEY" })).toBeInTheDocument();
    expect(screen.getByTestId("token-chips")).toHaveTextContent("Created on FactoryTrading on the DEXhoney.example");
    expect(screen.getByTestId("token-created")).toHaveTextContent("Created Oct 2, 2099 by3Gbze13gz…yhqk");
    expect(screen.getByRole("link", { name: /View on the DEX/ })).toHaveAttribute("href", expect.stringContaining("uhoney"));
    expect(screen.getAllByTestId("tile").map((t) => t.textContent)).toEqual([
      "Total supply500K HONEYcan change · see below",
      "Holders310accounts with at least 1 HONEY",
      "Price$0.002on the DEX",
      "Moved in 24h125K HONEY14 transfers",
    ]);

    const box = screen.getByTestId("supply-box");
    expect(box).toHaveAttribute("data-state", "admin");
    expect(box).toHaveTextContent(
      "The supply of HONEY can changeThe token's admin, 3Gbze13gz…yhqk, can create more HONEY or destroy some at any time. So far: 2 mints adding 1,000,500 HONEY, 1 burn removing 200 HONEY.",
    );

    const about = screen.getByTestId("token-about");
    expect(about).toHaveTextContent("NameHoney");
    expect(about).toHaveTextContent("CreatedOct 2, 2099 · see transaction");
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
    expect(screen.getByTestId("token-about")).toHaveTextContent("Adminrenounced: nobody can mint or burn");
  });

  it("renders a cross-chain token with its origin chain", async () => {
    renderWithProviders(<TokenPage denom={usdc.denom} />, { api: api(usdc) });
    expect(await screen.findByRole("heading", { level: 1, name: "USDC" })).toBeInTheDocument();
    expect(screen.getByTestId("token-chips")).toHaveTextContent("Cross-chain from Noble");
    expect(screen.queryByTestId("token-created")).not.toBeInTheDocument();
    const box = screen.getByTestId("supply-box");
    expect(box).toHaveAttribute("data-state", "elsewhere");
    expect(box).toHaveTextContent("USDC is minted on Noble");
    const about = screen.getByTestId("token-about");
    expect(about).toHaveTextContent("Origin chainNoble");
    expect(about).toHaveTextContent("Base denom thereuusdc");
    expect(about).toHaveTextContent("Pathtransfer/channel-1");
    expect(about).not.toHaveTextContent("Admin");
    expect(await screen.findByText("No activity indexed yet.")).toBeInTheDocument();
  });

  it("keeps the full name, base unit and raw metadata in More details", async () => {
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api() });
    await userEvent.click(await screen.findByRole("button", { name: /More details/ }));
    expect(screen.getByTestId("token-denom")).toHaveTextContent(honey.denom);
    expect(screen.getByText("1 HONEY = 1,000,000 uhoney")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy JSON" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open on Ping.pub/ })).toBeInTheDocument();
  });

  it("answers not found for a denom the API does not know", async () => {
    const token = vi.fn(() => Promise.reject(new ApiError(404, "not_found", "unknown denom")));
    renderWithProviders(<TokenPage denom="uzzz" />, { api: api(honey, { token }) });
    expect(await screen.findByRole("heading", { name: "Token not found" })).toBeInTheDocument();
  });

  it("offers a retry when the API fails", async () => {
    const token = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<TokenPage denom={honey.denom} />, { api: api(honey, { token }) });
    await userEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(token).toHaveBeenCalledTimes(2);
  });
});
