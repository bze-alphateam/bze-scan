import { act, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { BlockSummary, Page, TokenSummary, TxSummary, ValidatorList } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import blocks from "../../../test/fixtures/api/v1/blocks.json";
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
    blocks: () => Promise.resolve(blocks as Page<BlockSummary>),
    transactions: () => Promise.resolve(txs as Page<TxSummary>),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    validators: () => Promise.resolve(validators as ValidatorList),
    ...extra,
  });
}

const fail = () => Promise.reject(new ApiError(500, "internal", "boom"));

describe("HomePage", () => {
  it("shows five rows in each list as skeletons on the first load, and no figure tiles", () => {
    renderWithProviders(<HomePage />, { api: pendingApi() });
    expect(within(screen.getByTestId("latest-blocks")).getAllByTestId("list-skeleton-row")).toHaveLength(5);
    expect(within(screen.getByTestId("latest-txs")).getAllByTestId("list-skeleton-row")).toHaveLength(5);
    expect(screen.queryByTestId("tile")).not.toBeInTheDocument();
    expect(screen.queryByTestId("tile-skeleton")).not.toBeInTheDocument();
  });

  it("titles the page and labels the sections", () => {
    renderWithProviders(<HomePage />, { api: api() });
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Explore the BeeZee network");
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual(["Latest blocks", "Latest transactions", "Go to"]);
    expect(screen.getByRole("combobox", { name: "Search the explorer" })).toBeInTheDocument();
  });

  it("keeps both lists up when a list route fails on another", async () => {
    renderWithProviders(<HomePage />, { api: api({ transactions: fail }) });
    expect(await screen.findAllByTestId("latest-block")).toHaveLength(5);
    expect(await within(screen.getByTestId("latest-txs")).findByText(/could not be loaded/)).toBeInTheDocument();
  });

  it("keeps the last rows when a refetch fails", async () => {
    vi.useFakeTimers();
    let up = true;
    renderWithProviders(<HomePage />, { api: api({ blocks: () => (up ? Promise.resolve(blocks as Page<BlockSummary>) : fail()) }) });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getAllByTestId("latest-block")).toHaveLength(5);
    up = false;
    await act(() => vi.advanceTimersByTimeAsync(7_010));
    expect(screen.getAllByTestId("latest-block")).toHaveLength(5);
  });

  it("lists the latest blocks with their proposer and transactions", async () => {
    renderWithProviders(<HomePage />, { api: api() });
    const rows = await screen.findAllByTestId("latest-block");
    expect(rows).toHaveLength(5);
    expect(within(rows[0]).getByRole("link", { name: "25,120,449" })).toHaveAttribute("href", "/blocks/25120449");
    expect(rows[0]).toHaveTextContent(/by.*Honeycomb.*transactions?$/);
    expect(within(rows[0]).getByRole("link", { name: "Honeycomb" })).toBeInTheDocument();
    expect(within(screen.getByTestId("latest-blocks")).getByRole("link", { name: "View all blocks" })).toHaveAttribute("href", "/blocks");
  });

  it("lists the latest transactions with their title, hash and who dealt with whom", async () => {
    renderWithProviders(<HomePage />, { api: api() });
    const rows = await screen.findAllByTestId("latest-tx");
    expect(rows).toHaveLength(5);
    expect(within(rows[0]).getByTestId("latest-tx-what")).toHaveTextContent("Send 25 BZE to bze1m3k…9ds4");
    expect(within(rows[0]).getByTestId("latest-tx-what")).toHaveAttribute("href", `/txs/${txs.items[0].hash}`);
    expect(within(rows[0]).getByRole("link", { name: "27CE…916B" })).toBeInTheDocument();
    expect(within(rows[0]).getByTestId("latest-tx-parties")).toHaveTextContent("Q8bze1q8f…x0rq→M3bze1m3k…9ds4");
    expect(within(rows[1]).getByTestId("latest-tx-parties")).toHaveTextContent(/→HOHoneycomb$/);
    expect(within(rows[2]).getByTestId("latest-tx-parties")).toHaveTextContent(/→Burner$/);
    expect(within(rows[2]).getByText("Burner")).toHaveAttribute("data-module");
    expect(within(rows[3]).getByTestId("latest-tx-parties")).not.toHaveTextContent("→");
    expect(within(screen.getByTestId("latest-txs")).getByRole("link", { name: "View all transactions" })).toHaveAttribute("href", "/txs");
  });

  it("links to the other BZE apps", async () => {
    renderWithProviders(<HomePage />, { api: api() });
    const goTo = screen.getByRole("navigation", { name: "BZE apps" });
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
