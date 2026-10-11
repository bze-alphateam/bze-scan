import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { Page, TokenSummary } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import list from "../../../test/fixtures/api/v1/tokens.json";

import { TokensPage } from "./tokens-page";

const api = (tokens: ExplorerApi["tokens"] = () => Promise.resolve(list as Page<TokenSummary>)) => fakeApi({ tokens });
const symbols = () => screen.getAllByTestId("token-list-row").map((r) => within(r).getByTestId("token-link").textContent);

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"], now: new Date("2099-10-11T10:00:00Z") });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("TokensPage", () => {
  it("shows four tile skeletons and ten row skeletons on the first load", () => {
    renderWithProviders(<TokensPage />, { api: pendingApi() });
    expect(screen.getAllByTestId("tile-skeleton")).toHaveLength(4);
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(10);
  });

  it("renders the tiles and every token with its origin, supply, holders and price", async () => {
    renderWithProviders(<TokensPage />, { api: api() });
    expect(await screen.findByText("Every token on BZE. Prices come from the BZE DEX.")).toBeInTheDocument();
    expect(screen.getAllByTestId("tile").map((t) => t.textContent)).toEqual([
      "Tokens on BZE61 native · 2 Factory · 2 cross-chain · 1 pool share",
      "On the DEX4with a price on the DEX ",
      "Created this month1on Factory ",
      "Cross-chain2 chainsCosmos Hub, Noble",
    ]);
    expect(symbols()).toEqual(["BZE", "HONEY", "VDL", "ATOM", "USDC", "BZE/USDC LP"]);
    const [bze, honey, vdl, atom, usdc] = screen.getAllByTestId("token-list-row");
    expect(bze).toHaveTextContent("Native");
    expect(bze).toHaveTextContent("1B BZE");
    expect(bze).toHaveTextContent("12,000");
    expect(bze).toHaveTextContent("$0.0015 +1.2%");
    expect(within(honey).getByTestId("token-link")).toHaveAttribute(
      "href",
      "/tokens/factory%2Fbze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk%2Fuhoney",
    );
    expect(within(honey).getByRole("link", { name: /View on DEX/ })).toHaveAttribute(
      "href",
      expect.stringContaining("denom=factory%2Fbze13gzq"),
    );
    expect(vdl).toHaveTextContent("— no market");
    expect(within(vdl).queryByRole("link", { name: /View on DEX/ })).not.toBeInTheDocument();
    expect(atom).toHaveTextContent("Cross-chain · Cosmos Hub");
    expect(atom).toHaveTextContent("— Halted on the DEX");
    expect(usdc).toHaveTextContent("$1.00");
    // The raw denom stays in each token's More details.
    expect(screen.queryByText(/factory\/bze1/)).not.toBeInTheDocument();
  });

  it("filters by chip and by name", async () => {
    renderWithProviders(<TokensPage />, { api: api() });
    await userEvent.click(await screen.findByRole("button", { name: "Cross-chain" }));
    expect(screen.getByRole("button", { name: "Cross-chain" })).toHaveAttribute("aria-pressed", "true");
    expect(symbols()).toEqual(["ATOM", "USDC"]);
    await userEvent.click(screen.getByRole("button", { name: "Created on Factory" }));
    expect(symbols()).toEqual(["HONEY", "VDL"]);
    await userEvent.type(screen.getByRole("textbox", { name: "Find a token" }), "vid");
    expect(symbols()).toEqual(["VDL"]);
    await userEvent.click(screen.getByRole("button", { name: "Native" }));
    expect(screen.getByText("No token has that name.")).toBeInTheDocument();
    // The tiles always count every token.
    expect(screen.getAllByTestId("tile")[0]).toHaveTextContent("Tokens on BZE6");
  });

  it("follows the list's pages", async () => {
    const all = (list as Page<TokenSummary>).items;
    const tokens = vi.fn((p: { cursor?: string }) =>
      Promise.resolve(p.cursor ? { items: all.slice(3), next_cursor: null } : { items: all.slice(0, 3), next_cursor: "c1" }),
    );
    renderWithProviders(<TokensPage />, { api: api(tokens) });
    await screen.findByText("BZE/USDC LP");
    expect(tokens.mock.calls.map((c) => c[0])).toEqual([{ limit: 100, cursor: undefined }, { limit: 100, cursor: "c1" }]);
  });

  it("offers a retry when the API fails", async () => {
    const tokens = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<TokensPage />, { api: api(tokens) });
    await userEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(tokens).toHaveBeenCalledTimes(2);
  });
});
