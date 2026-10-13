import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { Account, ActivityPage, IbcTransfer, ModuleAccount, Page, Status, TokenSummary, ValidatorList } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import plain from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4.json";
import activity from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/activity.json";
import ibc from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/ibc.json";
import nodeDown from "../../../test/fixtures/api/v1/accounts/bze1q8f7z6x5c4v3d2n0mlkjhgfdsapzxu4a0ex0rq.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";
import modules from "../../../test/fixtures/api/v1/modules.json";
import status from "../../../test/fixtures/api/v1/status.json";
import list from "../../../test/fixtures/api/v1/validators.json";

import { AccountPage } from "./account-page";

const ADDR = plain.address;

const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
}));

function api(account: unknown = plain, extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    account: () => Promise.resolve(account as Account),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    validators: () => Promise.resolve(list as ValidatorList),
    accountActivity: () => Promise.resolve({ ...activity, total: 1530 } as ActivityPage),
    status: () => Promise.resolve(status as unknown as Status),
    accountIbc: () => Promise.resolve(ibc as Page<IbcTransfer>),
    ...extra,
  });
}

const tiles = () => screen.getAllByTestId("tile").map((t) => t.textContent?.trim());
const dex = modules.items[2];
const asModule = (address: string, name: string) => ({ ...plain, address, label: { name, kind: "module" } });
const withModules = (list: ModuleAccount[] = modules.items) => ({ modules: () => Promise.resolve({ items: list }) });

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"], now: new Date("2099-10-11T10:00:00Z") });
  window.history.replaceState(null, "", "/accounts/" + ADDR);
  push.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("AccountPage", () => {
  it("shows the header, tile and tab skeletons on the first load", () => {
    renderWithProviders(<AccountPage address={ADDR} />, { api: pendingApi() });
    expect(screen.getByTestId("account-skeleton")).toBeInTheDocument();
    expect(screen.getAllByTestId("tile-skeleton")).toHaveLength(4);
    expect(screen.getByTestId("tabs-skeleton")).toBeInTheDocument();
  });

  it("renders a plain account's header, tiles and tab strip", async () => {
    renderWithProviders(<AccountPage address={ADDR} />, { api: api() });
    expect(await screen.findByRole("heading", { level: 1, name: "Account" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Home/Account");
    expect(screen.getByTestId("kind-chip")).toHaveTextContent("Personal account");
    expect(screen.queryByTestId("label-chip")).not.toBeInTheDocument();
    expect(screen.getByTestId("account-since")).toHaveTextContent("active since Apr 2022 · 1,204 transactions");
    expect(screen.getByTestId("account-address")).toHaveTextContent(ADDR);
    expect(screen.getByRole("button", { name: "Copy address" })).toBeInTheDocument();

    await screen.findByText("≈ $1.85");
    expect(tiles()).toEqual([
      "Available1.2K BZE≈ $1.85",
      "Staked1K BZEwith 3 validators",
      "Rewards to claim15.75 BZEClaim on Staking ↗",
      "Unstaking300 BZEnext unlock in 9 days",
    ]);
    expect(screen.getByRole("link", { name: /Claim on Staking/ })).toHaveAttribute("href", "https://staking.getbze.com");

    expect(screen.getByRole("tab", { name: "Activity 1,530" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Tokens 5" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Staking 3 validators" })).toBeInTheDocument();
    expect(await screen.findAllByTestId("activity-row")).toHaveLength(8);
    expect(screen.getByTestId("activity-range")).toHaveTextContent("Activity 1–25 of 1,530");
  });

  it("offers no module menu on an account that is not a module", async () => {
    const modulesCall = vi.fn(() => Promise.resolve(modules as { items: ModuleAccount[] }));
    renderWithProviders(<AccountPage address={ADDR} />, { api: api(plain, { modules: modulesCall }) });
    await screen.findByRole("heading", { level: 1, name: "Account" });
    expect(screen.queryByTestId("module-switcher")).not.toBeInTheDocument();
    expect(modulesCall).not.toHaveBeenCalled();
  });

  it("shows a validator owner's validator picture", async () => {
    const withLogo = {
      ...list,
      items: list.items.map((v, i) => (i === 0 ? { ...v, logo_url: "https://pictures.example/honeycomb.jpg" } : v)),
    };
    renderWithProviders(<AccountPage address={ADDR} />, {
      api: api({ ...plain, label: { name: "Honeycomb", kind: "validator_owner" } }, {
        validators: () => Promise.resolve(withLogo as ValidatorList),
      }),
    });
    expect(await screen.findByTestId("avatar-image")).toHaveAttribute("src", "https://pictures.example/honeycomb.jpg");
  });

  it("links a validator owner to its validator", async () => {
    renderWithProviders(<AccountPage address={ADDR} />, {
      api: api({ ...plain, label: { name: "Honeycomb", kind: "validator_owner" } }),
    });
    const chip = await screen.findByTestId("label-chip");
    expect(await within(chip).findByRole("link", { name: "owner of Honeycomb" })).toHaveAttribute(
      "href",
      "/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c",
    );
  });

  it("says when an address was never seen, with its live balances still shown", async () => {
    const accountActivity = vi.fn();
    renderWithProviders(<AccountPage address={ADDR} />, {
      api: api({ ...plain, first_seen: null, tx_count: 0, activity_count: 0 }, { accountActivity }),
    });
    expect(await screen.findByText("no transactions seen yet")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Activity 0" })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByTestId("activity-never")).toHaveTextContent("No activity seen yet");
    expect(accountActivity).not.toHaveBeenCalled();
    expect(await screen.findByText("≈ $1.85")).toBeInTheDocument();
  });

  it("explains a node that did not answer instead of showing balances", async () => {
    renderWithProviders(<AccountPage address={nodeDown.address} />, { api: api(nodeDown) });
    expect(await screen.findByTestId("live-unavailable")).toHaveTextContent(
      "Live balances are unavailable right now: the node did not answer. Transactions and activity are still here.",
    );
    expect(screen.getAllByTestId("tile").map((t) => t.children[1].textContent)).toEqual(["—", "—", "—", "—"]);
    expect(screen.getByRole("tab", { name: "Activity 5" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Tokens" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Staking" })).toBeInTheDocument();
  });

  it("answers not a BZE account for an address the API refuses", async () => {
    const account = vi.fn(() => Promise.reject(new ApiError(400, "bad_request", "not a bze1 address")));
    renderWithProviders(<AccountPage address={ADDR} />, { api: api(plain, { account }) });
    expect(await screen.findByRole("heading", { name: "Not a BZE account" })).toBeInTheDocument();
    expect(screen.getByTestId("account-invalid")).toHaveAttribute("data-state", "error");
    expect(screen.getByTestId("account-invalid")).toHaveTextContent(
      "bze1m3k…9ds4 is not an address the explorer can read. Check that it starts with bze1 and was copied whole.",
    );
    expect(screen.getByRole("heading", { level: 1, name: "Account" })).toBeInTheDocument();
  });

  it("offers a retry when the API fails", async () => {
    const account = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<AccountPage address={ADDR} />, { api: api(plain, { account }) });
    expect(await screen.findByRole("heading", { name: "This account could not be loaded" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(account).toHaveBeenCalledTimes(2);
  });
});

describe("Module account", () => {
  it("is designed apart: dashed avatar and chip, the owner sentence, its own tiles and the callout", async () => {
    renderWithProviders(<AccountPage address={dex.address} />, { api: api(asModule(dex.address, "DEX"), withModules()) });
    expect(await screen.findByRole("heading", { level: 1, name: "DEX" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Home/Module account");
    const chip = screen.getByTestId("kind-chip");
    expect(chip).toHaveTextContent("Module account");
    expect(chip).toHaveAttribute("data-chip", "modc");
    expect(document.querySelector('[data-avatar="module"]')).toBeInTheDocument();
    expect(await screen.findByTestId("module-owner")).toHaveTextContent(
      "Owned by the chain's DEX module (tradebin). Holds the coins in liquidity pools and open orders.",
    );

    await screen.findAllByText("≈ $37.35");
    expect(tiles()).toEqual([
      "Holds≈ $37.355 tokens, at DEX prices",
      "BZE held1.2K BZEin pools and open orders",
      "Moves whenpeople tradeadd or remove liquidity, place, fill or cancel orders",
      "Who can signnobodyno private key exists",
    ]);
    expect(screen.getByTestId("module-callout")).toHaveTextContent(
      "This account belongs to the chain, not to a personNobody holds a key to it, so nobody can send from it. Its coins move only by the DEX's own rules, as a side effect of other people's transactions. It never stakes and earns no staking rewards.",
    );
    expect(screen.queryByTestId("account-since")).toHaveTextContent(/Owned by/);
  });

  it("opens on Holdings, offers Movements, and has no Activity or Staking tab", async () => {
    const accountActivity = vi.fn();
    renderWithProviders(<AccountPage address={dex.address} initialTab="staking" />, {
      api: api(asModule(dex.address, "DEX"), { ...withModules(), accountActivity }),
    });
    expect(await screen.findByRole("tab", { name: "Holdings 5" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Holdings 5", "Movements"]);
    expect(await screen.findAllByTestId("token-row")).toHaveLength(5);
    // Staking has no place on a module's holdings.
    expect(screen.queryByText(/staked ·/)).not.toBeInTheDocument();
    expect(screen.getByText("Tokens without a market on the DEX show no value.")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("tab", { name: "Movements" }));
    expect(window.location.search).toBe("?tab=movements");
    expect(screen.getByTestId("module-movements")).toHaveTextContent(
      "Module accounts don't send transactionsCoins move in and out of the DEX account inside other people's transactions.",
    );
    expect(screen.getByRole("link", { name: "See the latest transactions →" })).toHaveAttribute("href", "/txs");
    expect(accountActivity).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("tab", { name: "Holdings 5" }));
    expect(window.location.search).toBe("");
  });

  it("opens Movements from the URL", async () => {
    renderWithProviders(<AccountPage address={dex.address} initialTab="movements" />, { api: api(asModule(dex.address, "DEX"), withModules()) });
    expect(await screen.findByTestId("module-movements")).toBeInTheDocument();
  });

  it("falls back to plain words for a module the page does not know", async () => {
    const wasm = { address: ADDR, name: "Contracts", module: "wasm" };
    renderWithProviders(<AccountPage address={ADDR} />, {
      api: api(asModule(ADDR, "Contracts"), withModules([...modules.items, wasm])),
    });
    expect(await screen.findByTestId("module-owner")).toHaveTextContent("Owned by the chain's wasm module.");
    expect(screen.getAllByTestId("tile")[1]).toHaveTextContent("held by the module");
    expect(screen.getAllByTestId("tile")[2]).toHaveTextContent("Moves whenas the module's rules say");
  });

  it("explains a node that did not answer", async () => {
    renderWithProviders(<AccountPage address={dex.address} />, {
      api: api({ ...nodeDown, address: dex.address, label: { name: "DEX", kind: "module" } }, withModules()),
    });
    expect(await screen.findByTestId("live-unavailable")).toHaveTextContent("Live balances are unavailable right now");
    expect(screen.getByRole("tab", { name: "Holdings" })).toBeInTheDocument();
  });

  it("moves to the chain's other module accounts", async () => {
    renderWithProviders(<AccountPage address={dex.address} />, { api: api(asModule(dex.address, "DEX"), withModules()) });
    const select = await screen.findByTestId("module-switcher");
    expect(screen.getByLabelText("Other module accounts")).toBe(select);
    expect(select).toHaveValue(dex.address);
    expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual(modules.items.map((m) => m.name));

    await userEvent.selectOptions(select, "Burner");
    expect(push).toHaveBeenCalledWith("/accounts/bze1v7uw4xhrcv0vk7qp8jf9lu3hm5d8uu5yjp5qun");
  });
});

describe("Tokens tab", () => {
  it("opens from the URL, lists the tokens by value with origins and DEX links", async () => {
    renderWithProviders(<AccountPage address={ADDR} initialTab="tokens" />, { api: api() });
    const rows = await screen.findAllByTestId("token-row");
    expect(rows.map((r) => within(r).getByTestId("token-symbol").textContent)).toEqual([
      "USDC",
      "HONEY",
      "BZE",
      "BZE/USDC LP",
      "uzzz",
    ]);
    expect(screen.getByTestId("tokens-summary")).toHaveTextContent("5 tokens · ≈ $37.35 in total, at DEX prices");
    expect(within(rows[0]).getByTestId("token-badges")).toHaveTextContent("Cross-chainNoble");
    expect(within(rows[0]).getByTestId("direct-tag")).toHaveAttribute("title", "USDC comes from Noble, sent straight to BZE.");
    expect(rows[0]).toHaveTextContent("25.5 USDC");
    expect(rows[0]).toHaveTextContent("≈ $25.50");
    expect(within(rows[0]).getByRole("link", { name: /View on DEX/ })).toHaveAttribute(
      "href",
      expect.stringContaining("denom=ibc%2F6490A7EA"),
    );
    expect(rows[2]).toHaveTextContent("+ 1,000 BZE staked · 300 BZE unstaking");
    expect(rows[3]).toHaveTextContent("DEX pool share");
    expect(rows[3]).toHaveTextContent("no price");
    expect(within(rows[3]).queryByRole("link", { name: /View on DEX/ })).not.toBeInTheDocument();
    expect(within(rows[0]).getByTestId("token-symbol")).toHaveAttribute("href", expect.stringMatching(/^\/tokens\/ibc/));
    expect(within(rows[4]).getByTestId("token-symbol").tagName).not.toBe("A");
    expect(rows[4]).toHaveTextContent("42 uzzz");
  });

  it("sorts by name and keeps the tab in the URL", async () => {
    renderWithProviders(<AccountPage address={ADDR} />, { api: api() });
    await userEvent.click(await screen.findByRole("tab", { name: "Tokens 5" }));
    expect(window.location.search).toBe("?tab=tokens");
    await userEvent.click(await screen.findByRole("button", { name: "By name" }));
    const rows = screen.getAllByTestId("token-row");
    expect(rows.map((r) => within(r).getByTestId("token-symbol").textContent)).toEqual([
      "BZE",
      "BZE/USDC LP",
      "HONEY",
      "USDC",
      "uzzz",
    ]);
    await userEvent.click(screen.getByRole("tab", { name: "Activity 1,530" }));
    expect(window.location.search).toBe("");
  });

  it("pages through the tokens list", async () => {
    const all = (tokens as Page<TokenSummary>).items;
    const page = vi.fn((p: { cursor?: string }) =>
      Promise.resolve(p.cursor ? { items: all.slice(1), next_cursor: null } : { items: all.slice(0, 1), next_cursor: "c1" }),
    );
    renderWithProviders(<AccountPage address={ADDR} initialTab="tokens" />, { api: api(plain, { tokens: page }) });
    expect(await screen.findByTestId("tokens-summary")).toHaveTextContent("≈ $37.35");
    expect(page).toHaveBeenCalledTimes(2);
    expect(page.mock.calls[1][0]).toEqual({ limit: 100, cursor: "c1" });
  });

  it("shows row skeletons until the tokens list answers", async () => {
    renderWithProviders(<AccountPage address={ADDR} initialTab="tokens" />, {
      api: api(plain, { tokens: () => new Promise(() => {}) }),
    });
    await screen.findByRole("tab", { name: "Tokens 5" });
    expect(screen.getByTestId("table-skeleton")).toBeInTheDocument();
  });
});

describe("Staking tab", () => {
  it("lists the stake by validator with statuses, a total and what is unstaking", async () => {
    renderWithProviders(<AccountPage address={ADDR} initialTab="staking" />, { api: api() });
    const rows = await screen.findAllByTestId("delegation-row");
    expect(screen.getByTestId("staking-summary")).toHaveTextContent(
      "1,000 BZE staked with 3 validators · 15.75 BZE of rewards waiting",
    );
    expect(rows).toHaveLength(3);
    expect(within(rows[0]).getByRole("link", { name: "Honeycomb" })).toHaveAttribute(
      "href",
      "/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c",
    );
    expect(rows[0]).toHaveTextContent("Active");
    expect(rows[0]).toHaveTextContent("600 BZE");
    expect(rows[0]).toHaveTextContent("60%");
    expect(rows[0]).toHaveTextContent("12.5 BZE");
    expect(rows[1]).toHaveTextContent("98.4% uptime");
    expect(rows[2]).toHaveTextContent("Jailed");
    expect(rows[2]).toHaveTextContent("—");
    expect(screen.getByTestId("delegation-total")).toHaveTextContent("Total1,000 BZE100%15.75 BZE");

    const unbonding = screen.getAllByTestId("unbonding-item");
    expect(screen.getByRole("heading", { name: "Unstaking · 2" })).toBeInTheDocument();
    expect(unbonding[0]).toHaveTextContent(
      "250 BZE from Thamar Node · back in your account on Oct 30, 2099, 10:00:00 UTC (19 days) · no rewards while unstaking",
    );
    expect(screen.getByText(/Unstaking takes 21 days/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "the staking parameters" })).toHaveAttribute("href", "/parameters#staking");
  });
});
