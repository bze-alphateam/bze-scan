import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { SearchResult } from "@/api/types";
import { renderWithProviders } from "@/test/render";

import search from "../../../test/fixtures/api/v1/search.json";

import { SearchResultsPage } from "./search-results-page";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  usePathname: () => "/search",
  useRouter: () => ({ push: vi.fn(), refresh }),
}));

const ADDRESS = "bze1u7mzq0yk2wc8v5d3p6n9t4r7s0e2l5j8k4xh";

describe("SearchResultsPage", () => {
  it("lists the matches as links, validators first, with their count and type chips", () => {
    renderWithProviders(<SearchResultsPage q="hon" results={search.results as SearchResult[]} />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Results for “hon”");
    expect(screen.getByTestId("search-count")).toHaveTextContent("2 matches · validators first");
    const links = screen.getAllByTestId("search-result");
    expect(links.map((a) => a.getAttribute("href"))).toEqual([
      "/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c",
      "/accounts/bze1qqqsyqcyq5rqwzqfpg9scrgwpugpzysncvp6el",
    ]);
    expect(within(links[0]).getByText("Honeycomb")).toBeInTheDocument();
    expect(within(links[0]).getByText("validator")).toHaveAttribute("data-chip", "default");
    expect(within(links[1]).getByText("account")).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Search the explorer" })).toBeInTheDocument();
  });

  it("offers an address the explorer has not seen as Open account, not seen yet", () => {
    renderWithProviders(<SearchResultsPage q={ADDRESS} results={[]} account={ADDRESS} />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Results for “bze1u7m…k4xh”");
    const row = screen.getByTestId("search-result");
    expect(row).toHaveAttribute("href", `/accounts/${ADDRESS}`);
    expect(within(row).getByText("Open account")).toBeInTheDocument();
    expect(within(row).getByText("not seen yet")).toHaveAttribute("data-chip", "warn");
    expect(screen.getByText(/The address is valid but has no activity in the explorer's history/)).toBeInTheDocument();
    expect(screen.queryByTestId("search-count")).not.toBeInTheDocument();
  });

  it("asks for more when the query is too short", () => {
    renderWithProviders(<SearchResultsPage q="h" results={[]} />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Search");
    const box = screen.getByTestId("search-state");
    expect(box).toHaveAttribute("data-state", "neutral");
    expect(box).toHaveTextContent("Type a little more");
    expect(box).toHaveTextContent(/two letters or more/);
  });

  it("says when nothing matches", () => {
    renderWithProviders(<SearchResultsPage q="zzq" results={[]} />);
    const box = screen.getByTestId("search-state");
    expect(box).toHaveAttribute("data-state", "neutral");
    expect(box).toHaveTextContent("No validator or known account matches “zzq”");
    expect(box).toHaveTextContent("Search finds block heights, transaction hashes, addresses, validator names");
    expect(screen.queryByTestId("search-result")).not.toBeInTheDocument();
  });

  it("says when the search failed and tries again", async () => {
    renderWithProviders(<SearchResultsPage q="alpha" results={null} />);
    const box = screen.getByTestId("search-state");
    expect(box).toHaveAttribute("data-state", "error");
    expect(box).toHaveTextContent("The search could not be run");
    await userEvent.click(within(box).getByRole("button", { name: "Try again" }));
    expect(refresh).toHaveBeenCalled();
  });
});
