import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import type { ChainParams, ModuleAccount, Stats } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import modules from "../../../test/fixtures/api/v1/modules.json";
import params from "../../../test/fixtures/api/v1/params.json";
import stats from "../../../test/fixtures/api/v1/stats.json";

import { ParametersPage } from "./parameters-page";

const replace = vi.fn();
const router = { replace };
vi.mock("next/navigation", () => ({ useRouter: () => router }));

const api = (over: Partial<Parameters<typeof fakeApi>[0]> = {}) =>
  fakeApi({
    params: () => Promise.resolve(params as ChainParams),
    stats: () => Promise.resolve(stats as Stats),
    modules: () => Promise.resolve({ items: modules.items as ModuleAccount[] }),
    ...over,
  });

const shown = async () => (await screen.findByTestId("param-group")).id;
const row = (name: string) => screen.getByText(name).closest<HTMLElement>("[data-testid=param-row]")!;
const navItem = (label: string) => within(screen.getByTestId("param-nav")).getByRole("link", { name: new RegExp(`^${label}`) });

beforeEach(() => replace.mockClear());
afterEach(() => window.history.replaceState(null, "", "/"));

describe("ParametersPage", () => {
  it("lists every group in the nav and shows the selected one's header while loading", () => {
    renderWithProviders(<ParametersPage />, { api: pendingApi() });
    expect(screen.getByTestId("parameters-skeleton")).toHaveTextContent("Fees");
    expect(screen.getAllByTestId("param-nav-item")).toHaveLength(10);
    expect(navItem("Fees")).toHaveAttribute("aria-current", "true");
  });

  it("shows Fees with no ?group=, and only that group", async () => {
    renderWithProviders(<ParametersPage />, { api: api() });
    expect(await shown()).toBe("fees");
    expect(screen.getAllByTestId("param-group")).toHaveLength(1);
    expect(screen.queryByText("Unstaking period")).not.toBeInTheDocument();
    expect(navItem("Fees")).toHaveAttribute("aria-current", "true");
    expect(navItem("Staking")).toHaveAttribute("href", "/parameters?group=staking");
    expect(navItem("Staking")).not.toHaveAttribute("aria-current");
  });

  it("selects the group of ?group=, and falls back to Fees for an unknown one", async () => {
    const { unmount } = renderWithProviders(<ParametersPage group="staking" />, { api: api() });
    expect(await shown()).toBe("staking");
    expect(navItem("Staking")).toHaveAttribute("aria-current", "true");
    expect(screen.getByTestId("param-count")).toHaveTextContent(/^\d+ \+ \d+ advanced$/);
    unmount();

    renderWithProviders(<ParametersPage group="nope" />, { api: api() });
    expect(await shown()).toBe("fees");
  });

  it("selects the group of a legacy #anchor and moves it to ?group=", async () => {
    window.history.replaceState(null, "", "/parameters#dex");
    renderWithProviders(<ParametersPage />, { api: api() });
    expect(await shown()).toBe("dex");
    expect(replace).toHaveBeenCalledWith("/parameters?group=dex", { scroll: false });
  });

  it("switches the group in place and keeps it in the URL", async () => {
    renderWithProviders(<ParametersPage />, { api: api() });
    await shown();
    await userEvent.click(navItem("Governance"));
    expect(await shown()).toBe("gov");
    expect(replace).toHaveBeenCalledWith("/parameters?group=gov", { scroll: false });
    expect(navItem("Governance")).toHaveAttribute("aria-current", "true");
    expect(screen.getByRole("link", { name: "the proposals" })).toHaveAttribute("href", "/proposals");
  });

  it("opens a row's info: what it means, its chain name, the stored value and the proposal that changed it", async () => {
    renderWithProviders(<ParametersPage group="staking" />, { api: api() });
    await shown();
    const unbonding = row("Unstaking period");
    expect(within(unbonding).getByTestId("param-value")).toHaveTextContent("21 days");
    expect(screen.queryByTestId("param-info")).not.toBeInTheDocument();

    const button = within(unbonding).getByRole("button", { name: "Info about Unstaking period" });
    expect(button).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(button);
    const info = within(unbonding).getByTestId("param-info");
    expect(button).toHaveAttribute("aria-expanded", "true");
    expect(info).toHaveTextContent("Unstaked BZE stays locked for 21 days");
    expect(info).toHaveTextContent("On chainunbonding_time");
    expect(within(info).getByTestId("param-raw")).toHaveTextContent("1814400s");
    expect(within(info).getByRole("link", { name: "#11" })).toHaveAttribute("href", "/proposals/11");

    // A second row opens too; clicking the row itself closes it again.
    await userEvent.click(within(row("Active validator seats")).getByRole("button"));
    expect(screen.getAllByTestId("param-info")).toHaveLength(2);
    expect(row("Active validator seats")).toHaveTextContent("3 seats are taken right now.");
    await userEvent.click(within(unbonding).getByText("Unstaking period"));
    expect(screen.getAllByTestId("param-info")).toHaveLength(1);
    expect(row("Bond denom")).toHaveAttribute("data-advanced", "true");
  });

  it("shows module-account values as chips linking to the account", async () => {
    renderWithProviders(<ParametersPage group="dex" />, { api: api() });
    await shown();
    const burner = within(row("Where the fees go")).getByRole("link", { name: "Burner module · module" });
    expect(burner).toHaveAttribute("href", "/accounts/bze1v7uw4xhrcv0vk7qp8jf9lu3hm5d8uu5yjp5qun");
    // jsdom cannot follow the link; the row must not open either way.
    document.addEventListener("click", (e) => e.preventDefault(), { capture: true, once: true });
    await userEvent.click(burner);
    expect(screen.queryByTestId("param-info")).not.toBeInTheDocument();
  });

  it("lists the group's module accounts by the backend's names", async () => {
    renderWithProviders(<ParametersPage />, { api: api() });
    const block = await screen.findByTestId("module-accounts");
    expect(within(block).getByRole("heading")).toHaveTextContent("Module accounts");
    const rows = await within(block).findAllByTestId("module-account-row");
    expect(rows.map((r) => within(r).getAllByRole("link")[0].textContent)).toEqual([
      "FCFee collector",
      "SFStakers fee collector",
      "BFBurner fee collector",
      "CFCP fee collector",
    ]);
    const address = within(rows[0]).getByTestId("module-account-address");
    expect(address).toHaveAttribute("href", "/accounts/bze17xpfvakm2amg962yls6f84z3kell8c5lda0te2");
    expect(address).toHaveAttribute("title", "bze17xpfvakm2amg962yls6f84z3kell8c5lda0te2");
    expect(address).toHaveTextContent("bze17xp…0te2");
    expect(within(rows[0]).getByRole("button", { name: "Copy address" })).toBeInTheDocument();
  });

  it("names a single module account in the singular", async () => {
    renderWithProviders(<ParametersPage group="dex" />, { api: api() });
    const block = await screen.findByTestId("module-accounts");
    await within(block).findByTestId("module-account-row");
    expect(within(block).getByRole("heading")).toHaveTextContent(/^Module account$/);
    expect(within(block).getByRole("link", { name: "DEX · module" })).toBeInTheDocument();
  });

  it.each([
    ["penalties", "Slashing has no module account: penalties are taken straight from the validator's stake."],
    ["cointrunk", "CoinTrunk has no module account: article fees go straight to the community pool."],
  ])("says why %s has no module account", async (group, note) => {
    renderWithProviders(<ParametersPage group={group} />, { api: api() });
    const block = await screen.findByTestId("module-accounts");
    expect(block).toHaveTextContent(note);
    expect(within(block).queryByTestId("module-account-row")).not.toBeInTheDocument();
  });

  it("says not available for a module the chain did not answer", async () => {
    const rest = Object.fromEntries(Object.entries(params).filter(([m]) => m !== "tradebin")) as ChainParams;
    renderWithProviders(<ParametersPage group="dex" />, { api: api({ params: () => Promise.resolve(rest) }) });
    expect(await screen.findAllByText("not available")).toHaveLength(7);
    expect(screen.queryByTestId("parameters-skeleton")).not.toBeInTheDocument();
  });

  it("renders without /stats when it fails", async () => {
    renderWithProviders(<ParametersPage group="inflation" />, {
      api: api({ stats: () => Promise.reject(new ApiError(404, "not_found", "no stats")) }),
    });
    await screen.findByText("Inflation right now");
    expect(row("Inflation right now")).toHaveTextContent("not available");
  });

  it("copies the whole answer as JSON", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    renderWithProviders(<ParametersPage />, { api: api() });
    await user.click(await screen.findByRole("button", { name: "Copy all as JSON" }));
    expect(JSON.parse(writeText.mock.calls[0][0])).toEqual(params);
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open on Ping.pub/ })).toHaveAttribute("href", expect.stringMatching(/\/params$/));
  });

  it("offers a retry when the API fails", async () => {
    const fail = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<ParametersPage />, { api: api({ params: fail }) });
    await userEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(fail).toHaveBeenCalledTimes(2);
  });
});
