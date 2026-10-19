import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import { queryKeys } from "@/api/hooks";
import type { Account, ActivityPage, IbcTransfer, ModuleAccount, OpenOrders, Page, Status, TokenSummary, ValidatorList } from "@/api/types";
import { fakeApi, renderWithProviders } from "@/test/render";

import plain from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4.json";
import activity from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/activity.json";
import ibc from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/ibc.json";
import openOrders from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/open-orders.json";
import modules from "../../../test/fixtures/api/v1/modules.json";
import status from "../../../test/fixtures/api/v1/status.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";
import list from "../../../test/fixtures/api/v1/validators.json";

import { AccountPage } from "./account-page";

const ADDR = plain.address;
const o = openOrders as OpenOrders;
const HONEY_MARKET = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney/ubze";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

function api(orders: () => Promise<OpenOrders> = () => Promise.resolve(o), extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    account: () => Promise.resolve(plain as Account),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    validators: () => Promise.resolve(list as ValidatorList),
    accountActivity: () => Promise.resolve({ ...activity, total: 1530 } as ActivityPage),
    accountIbc: () => Promise.resolve(ibc as Page<IbcTransfer>),
    status: () => Promise.resolve(status as unknown as Status),
    accountOpenOrders: orders,
    ...extra,
  });
}

const never = () => new Promise<OpenOrders>(() => {});
const failing = () => Promise.reject(new ApiError(502, "upstream_error", "the nodes did not answer"));
const symbols = () => screen.getAllByTestId("order-symbol").map((s) => s.textContent);

beforeEach(() => {
  window.history.replaceState(null, "", "/accounts/" + ADDR);
});

describe("Orders tab", () => {
  it("has no count until the open orders answer, then their number", async () => {
    let answer: (v: OpenOrders) => void = () => {};
    const orders = () => new Promise<OpenOrders>((r) => (answer = r));
    renderWithProviders(<AccountPage address={ADDR} />, { api: api(orders) });
    expect(await screen.findByRole("tab", { name: "Orders" })).toBeInTheDocument();
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Activity 1,530", "Tokens 5", "Orders", "Staking 3 validators"]);
    answer(o);
    expect(await screen.findByRole("tab", { name: "Orders 6" })).toBeInTheDocument();
  });

  it("asks for the open orders with the page, on the slow policy, whatever the tab", async () => {
    const orders = vi.fn(() => Promise.resolve(o));
    const { queryClient } = renderWithProviders(<AccountPage address={ADDR} />, { api: api(orders) });
    await screen.findByRole("tab", { name: "Orders 6" });
    expect(orders).toHaveBeenCalledTimes(1);
    const query = queryClient.getQueryCache().find({ queryKey: queryKeys.accountOpenOrders(ADDR) });
    expect(query?.options).toMatchObject({ staleTime: 30_000, refetchInterval: 30_000, refetchIntervalInBackground: false });
  });

  it("lists the assets by value with their counts, collapsed, and the priced total", async () => {
    renderWithProviders(<AccountPage address={ADDR} initialTab="orders" />, { api: api() });
    expect(await screen.findByTestId("orders-summary")).toHaveTextContent("Locked in 6 open orders · 3 markets");
    expect(screen.getByTestId("orders-total")).toHaveTextContent("≈ $2.68 of the priced tokens");
    expect(symbols()).toEqual(["HONEY", "BZE", "VDL"]);
    const rows = screen.getAllByTestId("order-asset-row");
    expect(within(rows[0]).getByTestId("order-amount")).toHaveTextContent("1,000 HONEY");
    expect(within(rows[0]).getByTestId("order-value")).toHaveTextContent("≈ $2.00");
    expect(within(rows[0]).getByTestId("order-counts")).toHaveTextContent("2 orders · 1 market");
    expect(within(rows[1]).getByTestId("order-counts")).toHaveTextContent("3 orders · 2 markets");
    expect(within(rows[2]).getByTestId("order-value")).toHaveTextContent("no price");
    expect(within(rows[0]).getByTestId("order-symbol")).toHaveAttribute("href", expect.stringMatching(/^\/tokens\/factory/));
    expect(screen.queryByTestId("order-market-row")).not.toBeInTheDocument();
    for (const b of screen.getAllByTestId("order-toggle")) expect(b).toHaveAttribute("aria-expanded", "false");
  });

  it("expands an asset to its markets, each with its amount, count and DEX link", async () => {
    renderWithProviders(<AccountPage address={ADDR} initialTab="orders" />, { api: api() });
    const toggle = await screen.findByRole("button", { name: "Show the markets holding BZE" });
    expect(toggle.tagName).toBe("BUTTON");
    await userEvent.click(toggle);
    expect(screen.getByRole("button", { name: "Hide the markets holding BZE" })).toHaveAttribute("aria-expanded", "true");
    const markets = within(screen.getByRole("list", { name: "Markets holding BZE" })).getAllByTestId("order-market-row");
    expect(markets).toHaveLength(2);
    expect(within(markets[0]).getByTestId("order-market")).toHaveTextContent("BZE/USDC");
    expect(markets[0]).toHaveTextContent("300 BZE");
    expect(markets[0]).toHaveTextContent("2 orders");
    expect(within(markets[1]).getByTestId("order-market")).toHaveTextContent("HONEY/BZE");
    const link = within(markets[1]).getAllByRole("link", { name: "DEX" })[0];
    expect(link).toHaveAttribute("data-testid", "app-link");
    expect(link).toHaveAttribute("data-app", "dex");
    expect(link).toHaveAttribute("href", `https://dex.getbze.com/exchange/market?id=${encodeURIComponent(HONEY_MARKET)}`);
    expect(link).toHaveAttribute("target", "_blank");

    await userEvent.click(screen.getByRole("button", { name: "Hide the markets holding BZE" }));
    expect(screen.queryByTestId("order-market-row")).not.toBeInTheDocument();
  });

  it("opens a single asset by default", async () => {
    const one = { ...o, orders_count: 2, markets_count: 1, assets: [o.assets[0]] };
    renderWithProviders(<AccountPage address={ADDR} initialTab="orders" />, { api: api(() => Promise.resolve(one)) });
    expect(await screen.findByRole("button", { name: "Hide the markets holding HONEY" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getAllByTestId("order-market-row")).toHaveLength(1);
    expect(screen.getByTestId("orders-summary")).toHaveTextContent("Locked in 2 open orders · 1 market");
    expect(screen.getByTestId("orders-total")).toHaveTextContent(/^≈ \$2\.00$/);
  });

  it("says so when the account has no open orders", async () => {
    const none = { ...o, orders_count: 0, markets_count: 0, assets: [] };
    renderWithProviders(<AccountPage address={ADDR} initialTab="orders" />, { api: api(() => Promise.resolve(none)) });
    expect(await screen.findByTestId("orders-empty")).toHaveTextContent("This account has no open orders on the DEX.");
    expect(screen.getByRole("tab", { name: "Orders 0" })).toBeInTheDocument();
  });

  it("shows a skeleton while the open orders load, and the rest of the page meanwhile", async () => {
    renderWithProviders(<AccountPage address={ADDR} initialTab="orders" />, { api: api(never) });
    expect(await screen.findByTestId("table-skeleton")).toBeInTheDocument();
    expect(await screen.findByText("≈ $1.85")).toBeInTheDocument();
  });

  it("keeps a failed answer to the Orders panel", async () => {
    renderWithProviders(<AccountPage address={ADDR} initialTab="orders" />, { api: api(failing) });
    expect(await screen.findByTestId("orders-error")).toHaveTextContent("The open orders could not be loaded right now.");
    expect(screen.getByRole("tab", { name: "Orders" })).toBeInTheDocument();
    expect(await screen.findByText("≈ $1.85")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("tab", { name: "Tokens 5" }));
    expect(screen.getAllByTestId("token-row")).toHaveLength(5);
    expect(screen.queryByTestId("token-in-orders")).not.toBeInTheDocument();
  });

  it("is not on a module account, which never asks for open orders", async () => {
    const dex = modules.items[2];
    const orders = vi.fn(() => Promise.resolve(o));
    renderWithProviders(<AccountPage address={dex.address} />, {
      api: api(orders, {
        account: () => Promise.resolve({ ...plain, address: dex.address, label: { name: "DEX", kind: "module" } } as Account),
        modules: () => Promise.resolve(modules as { items: ModuleAccount[] }),
      }),
    });
    expect(await screen.findByRole("tab", { name: "Holdings 5" })).toBeInTheDocument();
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Holdings 5", "Movements"]);
    expect(screen.queryByTestId("token-in-orders")).not.toBeInTheDocument();
    expect(orders).not.toHaveBeenCalled();
  });
});

describe("Tokens tab, in orders", () => {
  it("adds a line under the balances that have orders, leaving values, total and rows bank-only", async () => {
    renderWithProviders(<AccountPage address={ADDR} initialTab="tokens" />, { api: api() });
    await screen.findAllByTestId("token-in-orders");
    expect(screen.getByTestId("tokens-summary")).toHaveTextContent("5 tokens · ≈ $37.35 in total, at DEX prices");
    const rows = screen.getAllByTestId("token-row");
    expect(rows).toHaveLength(5);
    const lines = rows.map((r) => within(r).queryByTestId("token-in-orders")?.textContent ?? null);
    const bySymbol = Object.fromEntries(rows.map((r, i) => [within(r).getByTestId("token-symbol").textContent, lines[i]]));
    expect(bySymbol).toMatchObject({ HONEY: "+ 1,000 HONEY in orders", BZE: "+ 450 BZE in orders", USDC: null });
    // VDL is only in orders: it gets no Tokens row.
    expect(Object.keys(bySymbol)).not.toContain("VDL");
    expect(screen.getByText(/Tokens locked in DEX orders are counted in the Orders tab\./)).toBeInTheDocument();
  });

  it("shows the balances without waiting for the open orders", async () => {
    renderWithProviders(<AccountPage address={ADDR} initialTab="tokens" />, { api: api(never) });
    expect(await screen.findByTestId("tokens-summary")).toHaveTextContent("≈ $37.35");
    expect(screen.getAllByTestId("token-row")).toHaveLength(5);
    expect(screen.queryByTestId("token-in-orders")).not.toBeInTheDocument();
  });

  it("opens the Orders tab from a line", async () => {
    renderWithProviders(<AccountPage address={ADDR} initialTab="tokens" />, { api: api() });
    const [line] = await screen.findAllByTestId("token-in-orders");
    await userEvent.click(line);
    expect(screen.getByRole("tab", { name: "Orders 6" })).toHaveAttribute("aria-selected", "true");
    expect(window.location.search).toBe("?tab=orders");
    expect(await screen.findByTestId("orders-summary")).toBeInTheDocument();
  });
});
