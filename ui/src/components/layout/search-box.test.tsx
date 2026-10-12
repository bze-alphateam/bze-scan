import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import type { SearchResponse } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import search from "../../../test/fixtures/api/v1/search.json";

import { SearchBox } from "./search-box";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  usePathname: () => "/",
  useRouter: () => ({ push }),
}));

beforeEach(() => push.mockReset());

const ACCOUNT = "bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4";
const VALOPER = "bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c";
const HASH = "3f9a6b2c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d0c21e";

const box = () => screen.getByRole("combobox", { name: "Search the explorer" });

function withSearch(answer: (q: string) => Promise<SearchResponse> = () => Promise.resolve(search as SearchResponse)) {
  const fetchSearch = vi.fn(answer);
  renderWithProviders(<SearchBox />, { api: fakeApi({ search: fetchSearch }) });
  return fetchSearch;
}

describe("SearchBox", () => {
  it("opens an identifier's page without asking the API", async () => {
    const fetchSearch = withSearch();
    for (const [typed, href] of [
      ["25120440", "/blocks/25120440"],
      [HASH, `/txs/${HASH.toUpperCase()}`],
      [VALOPER, `/validators/${VALOPER}`],
    ]) {
      await userEvent.type(box(), `${typed}{Enter}`);
      expect(push).toHaveBeenLastCalledWith(href);
      expect(box()).toHaveValue("");
    }
    expect(fetchSearch).not.toHaveBeenCalled();
  });

  it("lists validators first, then accounts, after a pause in typing", async () => {
    const fetchSearch = withSearch();
    await userEvent.type(box(), "hon");
    const options = await screen.findAllByRole("option");
    expect(fetchSearch).toHaveBeenCalledTimes(1);
    expect(fetchSearch).toHaveBeenCalledWith("hon", expect.anything());
    expect(options.map((o) => o.textContent)).toEqual([
      expect.stringMatching(/Honeycomb.*validator$/),
      expect.stringMatching(/Honey Market.*account$/),
    ]);
    expect(box()).toHaveAttribute("aria-expanded", "true");
  });

  it("moves through the results with the arrows and opens the chosen one with Enter", async () => {
    withSearch();
    await userEvent.type(box(), "hon");
    await screen.findAllByRole("option");
    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    const options = screen.getAllByRole("option");
    expect(options[1]).toHaveAttribute("aria-selected", "true");
    expect(box()).toHaveAttribute("aria-activedescendant", options[1].id);
    await userEvent.keyboard("{ArrowUp}{Enter}");
    expect(push).toHaveBeenCalledWith(`/validators/${VALOPER}`);
  });

  it("opens the first result on Enter, and a clicked one", async () => {
    withSearch();
    await userEvent.type(box(), "hon");
    await screen.findAllByRole("option");
    await userEvent.keyboard("{Enter}");
    expect(push).toHaveBeenLastCalledWith(`/validators/${VALOPER}`);

    await userEvent.type(box(), "hon");
    await userEvent.click((await screen.findAllByRole("option"))[1]);
    expect(push).toHaveBeenLastCalledWith("/accounts/bze1qqqsyqcyq5rqwzqfpg9scrgwpugpzysncvp6el");
  });

  it("closes on Escape and on blur", async () => {
    withSearch();
    await userEvent.type(box(), "hon");
    await screen.findAllByRole("option");
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByTestId("search-dropdown")).not.toBeInTheDocument();
    expect(box()).toHaveAttribute("aria-expanded", "false");

    await userEvent.type(box(), "e");
    expect(await screen.findByTestId("search-dropdown")).toBeInTheDocument();
    await userEvent.tab();
    expect(screen.queryByTestId("search-dropdown")).not.toBeInTheDocument();
  });

  it("says when nothing matches, and goes to the search page on Enter", async () => {
    withSearch(() => Promise.resolve({ results: [] }));
    await userEvent.type(box(), "zzz");
    expect(await screen.findByText("No validator or known account matches")).toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(push).toHaveBeenCalledWith("/search?q=zzz");
  });

  it("shows one skeleton line while the search is in flight", async () => {
    renderWithProviders(<SearchBox />, { api: pendingApi() });
    await userEvent.type(box(), "hon");
    expect(screen.getAllByTestId("search-skeleton")).toHaveLength(1);
    expect(screen.queryByRole("option")).not.toBeInTheDocument();
  });

  it("offers an address typed in full as Open account, not seen yet when unindexed", async () => {
    withSearch(() => Promise.resolve({ results: [{ type: "account", id: ACCOUNT, label: ACCOUNT, indexed: false }] }));
    await userEvent.type(box(), ACCOUNT);
    const [open] = screen.getAllByRole("option");
    expect(open).toHaveTextContent("Open account");
    expect(await within(open).findByText("not seen yet")).toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(push).toHaveBeenCalledWith(`/accounts/${ACCOUNT}`);
  });

  it("says when the search failed and still submits to the search page", async () => {
    withSearch(() => Promise.reject(new ApiError(500, "internal", "boom")));
    await userEvent.type(box(), "hon");
    expect(await screen.findByText(/The search could not be run/)).toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(push).toHaveBeenCalledWith("/search?q=hon");
  });

  it("does nothing for one character", async () => {
    const fetchSearch = withSearch();
    await userEvent.type(box(), "h{Enter}");
    expect(push).not.toHaveBeenCalled();
    expect(fetchSearch).not.toHaveBeenCalled();
    expect(screen.queryByTestId("search-dropdown")).not.toBeInTheDocument();
  });
});
