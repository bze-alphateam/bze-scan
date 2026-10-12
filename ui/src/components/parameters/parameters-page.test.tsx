import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import type { ChainParams, Proposal, Stats } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import params from "../../../test/fixtures/api/v1/params.json";
import proposal from "../../../test/fixtures/api/v1/proposals/45.json";
import stats from "../../../test/fixtures/api/v1/stats.json";

import { ParametersPage } from "./parameters-page";

const api = (over: Partial<Parameters<typeof fakeApi>[0]> = {}) =>
  fakeApi({
    params: () => Promise.resolve(params as ChainParams),
    stats: () => Promise.resolve(stats as Stats),
    proposal: (id) => Promise.resolve({ ...(proposal as Proposal), id, title: "V8 Upgrade" }),
    ...over,
  });

const group = (id: string) => screen.getAllByTestId("param-group").find((g) => g.id === id)!;

describe("ParametersPage", () => {
  it("shows the anchor nav and three skeleton rows per group on the first load", () => {
    renderWithProviders(<ParametersPage />, { api: pendingApi() });
    expect(screen.getByTestId("parameters-skeleton")).toBeInTheDocument();
    expect(within(screen.getByTestId("param-nav")).getAllByRole("link")).toHaveLength(10);
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(30);
  });

  it("renders every group with plain names, human and raw values, and what they mean", async () => {
    renderWithProviders(<ParametersPage />, { api: api() });
    expect(await screen.findByTestId("last-change")).toHaveTextContent("Last change #44 · Nov 24, 2025 · DEX, Rewards programs");
    expect(await screen.findByRole("link", { name: "#44 V8 Upgrade" })).toHaveAttribute("href", "/proposals/44");
    expect(screen.getAllByTestId("param-group").map((g) => g.id)).toEqual([
      "fees",
      "staking",
      "inflation",
      "penalties",
      "gov",
      "dex",
      "factory",
      "rewards",
      "burner",
      "cointrunk",
    ]);
    expect(within(screen.getByTestId("param-nav")).getByRole("link", { name: "Governance" })).toHaveAttribute("href", "#gov");

    const staking = group("staking");
    expect(within(staking).getByTestId("changed-by")).toHaveAttribute("href", "/proposals/11");
    const unbonding = within(staking).getAllByTestId("param-row")[0];
    expect(unbonding).toHaveTextContent("Unstaking period");
    expect(unbonding).toHaveTextContent("unbonding_time");
    expect(within(unbonding).getByTestId("param-value")).toHaveTextContent("21 days");
    expect(within(unbonding).getByTestId("param-raw")).toHaveTextContent("1814400s");
    expect(unbonding).toHaveTextContent("Unstaked BZE stays locked for 21 days");
    expect(within(staking).getAllByTestId("param-row")[1]).toHaveTextContent("3 seats are taken right now.");
    expect(within(staking).getByText("Bond denom").closest("[data-testid=param-row]")).toHaveAttribute("data-advanced", "true");

    // The fee destinations are the Burner's module account.
    const burner = within(group("dex")).getByRole("link", { name: "Burner module · module" });
    expect(burner).toHaveAttribute("href", "/accounts/bze1v7uw4xhrcv0vk7qp8jf9lu3hm5d8uu5yjp5qun");
    expect(within(group("dex")).getByTestId("changed-by")).toHaveTextContent("changed by #44");
    expect(within(group("gov")).getByRole("link", { name: "the proposals" })).toHaveAttribute("href", "/proposals");
    expect(screen.queryByText("not available")).not.toBeInTheDocument();
  });

  it("says not available for a module the chain did not answer", async () => {
    const rest = Object.fromEntries(Object.entries(params).filter(([m]) => m !== "tradebin")) as ChainParams;
    renderWithProviders(<ParametersPage />, { api: api({ params: () => Promise.resolve(rest) }) });
    expect(await screen.findAllByText("not available")).toHaveLength(7);
    expect(within(group("dex")).queryByTestId("changed-by")).not.toBeInTheDocument();
    expect(screen.queryByTestId("parameters-skeleton")).not.toBeInTheDocument();
  });

  it("renders without /stats when it fails", async () => {
    renderWithProviders(<ParametersPage />, {
      api: api({ stats: () => Promise.reject(new ApiError(404, "not_found", "no stats")) }),
    });
    const inflation = await screen.findByText("Inflation right now");
    expect(inflation.closest("[data-testid=param-row]")).toHaveTextContent("not available");
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
