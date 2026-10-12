import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { SearchResult } from "@/api/types";
import { renderWithProviders } from "@/test/render";

import search from "../../../test/fixtures/api/v1/search.json";

import { SearchResultsPage } from "./search-results-page";

vi.mock("next/navigation", () => ({
  usePathname: () => "/search",
  useRouter: () => ({ push: vi.fn() }),
}));

describe("SearchResultsPage", () => {
  it("lists the matches as links, validators first", () => {
    renderWithProviders(<SearchResultsPage q="hon" results={search.results as SearchResult[]} />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Results for “hon”");
    const links = screen.getAllByTestId("search-result");
    expect(links.map((a) => a.getAttribute("href"))).toEqual([
      "/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c",
      "/accounts/bze1qqqsyqcyq5rqwzqfpg9scrgwpugpzysncvp6el",
    ]);
  });

  it("says when nothing matches, when the search failed, and when the query is too short", () => {
    const { rerender } = renderWithProviders(<SearchResultsPage q="zzz" results={[]} />);
    expect(screen.getByText("No validator or known account matches")).toBeInTheDocument();
    rerender(<SearchResultsPage q="zzz" results={null} />);
    expect(screen.getByText(/The search could not be run/)).toBeInTheDocument();
    rerender(<SearchResultsPage q="h" results={[]} />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Search");
    expect(screen.getByText(/two letters or more/)).toBeInTheDocument();
  });
});
