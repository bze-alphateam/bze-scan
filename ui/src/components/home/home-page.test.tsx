import { act, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { BlockSummary, Page, Stats, TokenSummary, TxSummary, ValidatorList } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import blocks from "../../../test/fixtures/api/v1/blocks.json";
import stats from "../../../test/fixtures/api/v1/stats.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";
import txs from "../../../test/fixtures/api/v1/txs.json";
import validators from "../../../test/fixtures/api/v1/validators.json";

import { HomePage } from "./home-page";

vi.mock("next/navigation", () => ({
  usePathname: () => "/",
  useRouter: () => ({ push: vi.fn() }),
}));

afterEach(() => {
  vi.useRealTimers();
});

function api(extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    stats: () => Promise.resolve(stats as Stats),
    blocks: () => Promise.resolve(blocks as Page<BlockSummary>),
    transactions: () => Promise.resolve(txs as Page<TxSummary>),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    validators: () => Promise.resolve(validators as ValidatorList),
    ...extra,
  });
}

const tile = (label: string) => screen.getAllByTestId("tile").find((t) => t.firstChild?.textContent === label)!;
const fail = () => Promise.reject(new ApiError(500, "internal", "boom"));

describe("HomePage", () => {
  it("shows six tiles and five rows in each list as skeletons on the first load", () => {
    renderWithProviders(<HomePage />, { api: pendingApi() });
    expect(screen.getAllByTestId("tile-skeleton")).toHaveLength(6);
    expect(within(screen.getByTestId("latest-blocks")).getAllByTestId("list-skeleton-row")).toHaveLength(5);
    expect(within(screen.getByTestId("latest-txs")).getAllByTestId("list-skeleton-row")).toHaveLength(5);
  });

  it("renders the six tiles from the stats", async () => {
    renderWithProviders(<HomePage />, { api: api() });
    await screen.findByTestId("home-tiles");
    expect(screen.getAllByTestId("tile").map((t) => t.firstChild?.textContent)).toEqual([
      "Latest block",
      "Block time",
      "Transactions",
      "Validators",
      "Staked",
      "BZE price",
    ]);
    expect(tile("Latest block")).toHaveTextContent("25,120,450");
    expect(tile("Latest block").querySelector("time")).toHaveAttribute("datetime", stats.latest_time);
    expect(tile("Validators")).toHaveTextContent("3active · 40 seats");
    expect(tile("BZE price")).toHaveTextContent("$0.0015+1.2% today");
  });

  it("says no price yet when the price is null", async () => {
    renderWithProviders(<HomePage />, { api: api({ stats: () => Promise.resolve({ ...stats, price_usd: null } as Stats) }) });
    await screen.findByTestId("home-tiles");
    expect(tile("BZE price")).toHaveTextContent("—no price yet");
  });

  it("keeps the page up when the stats route fails, and the last values when a refetch fails", async () => {
    renderWithProviders(<HomePage />, { api: api({ stats: fail }) });
    await screen.findByTestId("home-tiles");
    expect(tile("Latest block")).toHaveTextContent("Latest block—");
    expect(await screen.findAllByTestId("latest-block")).toHaveLength(5);

    vi.useFakeTimers();
    let up = true;
    renderWithProviders(<HomePage />, { api: api({ stats: () => (up ? Promise.resolve(stats as Stats) : fail()) }) });
    await act(() => vi.advanceTimersByTimeAsync(0));
    const latest = () => screen.getAllByTestId("home-tiles").at(-1)!;
    expect(latest()).toHaveTextContent("25,120,450");
    up = false;
    await act(() => vi.advanceTimersByTimeAsync(7_010));
    expect(latest()).toHaveTextContent("25,120,450");
  });

  it("lists the latest blocks with their proposer and transactions", async () => {
    renderWithProviders(<HomePage />, { api: api() });
    const rows = await screen.findAllByTestId("latest-block");
    expect(rows).toHaveLength(5);
    expect(within(rows[0]).getByRole("link", { name: "25,120,449" })).toHaveAttribute("href", "/blocks/25120449");
    expect(rows[0]).toHaveTextContent("by");
    expect(within(rows[0]).getByRole("link", { name: "Honeycomb" })).toBeInTheDocument();
    expect(within(screen.getByTestId("latest-blocks")).getByRole("link", { name: "View all blocks" })).toHaveAttribute("href", "/blocks");
  });

  it("lists the latest transactions with their title, hash and who dealt with whom", async () => {
    renderWithProviders(<HomePage />, { api: api() });
    const rows = await screen.findAllByTestId("latest-tx");
    expect(rows).toHaveLength(5);
    expect(within(rows[0]).getByTestId("latest-tx-what")).toHaveTextContent("Send 25 BZE to bze1m3k…9ds4");
    expect(within(rows[0]).getByRole("link", { name: "27CE…916B" })).toBeInTheDocument();
    expect(within(rows[0]).getByTestId("latest-tx-parties")).toHaveTextContent("Q8bze1q8f…x0rq→M3bze1m3k…9ds4");
    expect(within(rows[1]).getByTestId("latest-tx-parties")).toHaveTextContent(/→HOHoneycomb$/);
    expect(within(rows[2]).getByTestId("latest-tx-parties")).toHaveTextContent(/→Burner$/);
    expect(within(rows[3]).getByTestId("latest-tx-parties")).not.toHaveTextContent("→");
    expect(within(screen.getByTestId("latest-txs")).getByRole("link", { name: "View all transactions" })).toHaveAttribute("href", "/txs");
  });

  it("links to the other BZE apps", async () => {
    renderWithProviders(<HomePage />, { api: api() });
    const goTo = screen.getByRole("heading", { name: "Go to" }).parentElement!;
    expect(within(goTo).getAllByRole("link").map((a) => a.textContent)).toEqual([
      "Website",
      "Staking",
      "DEX",
      "Burner",
      "Factory",
      "Communities",
      "Docs",
    ]);
  });
});
