import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { Dao, DaoProposalSummary, Page } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import dao2 from "../../../test/fixtures/api/v1/daos/2.json";
import list2 from "../../../test/fixtures/api/v1/daos/2/proposals.json";

import { DaoProposalsTab } from "./dao-proposals-tab";

const page = list2 as Page<DaoProposalSummary>;
const dao = dao2 as Dao;

function render(extra: Partial<ExplorerApi> = {}) {
  return renderWithProviders(<DaoProposalsTab dao={dao} />, { api: fakeApi({ daoProposals: () => Promise.resolve(page), ...extra }) });
}

describe("DaoProposalsTab", () => {
  it("shows a skeleton while loading", () => {
    renderWithProviders(<DaoProposalsTab dao={dao} />, { api: pendingApi() });
    expect(screen.getByTestId("table-skeleton")).toBeInTheDocument();
  });

  it("lists #id, title and status, each linking to the proposal under its DAO", async () => {
    render();
    const rows = await screen.findAllByTestId("dao-proposal-row");
    expect(rows.map((r) => r.getAttribute("data-status"))).toEqual(["voting", "executed", "execution_failed"]);
    const first = within(rows[0]).getByRole("link");
    expect(first).toHaveTextContent("#7Fund the hive festival");
    expect(first).toHaveAttribute("href", "/daos/2/proposals/7");
    expect(within(rows[0]).getByTestId("dao-proposal-status")).toHaveTextContent("Voting");
    expect(within(rows[1]).getByTestId("dao-proposal-status")).toHaveTextContent("Passed (executed)");
    expect(within(rows[2]).getByTestId("dao-proposal-status")).toHaveTextContent("Failed to execute");
  });

  it("says when voting ends, or when it ended", async () => {
    render();
    const rows = await screen.findAllByTestId("dao-proposal-row");
    expect(within(rows[0]).getByTestId("dao-proposal-end")).toHaveTextContent(/^ends in \d+ days/);
    expect(within(rows[1]).getByTestId("dao-proposal-end")).toHaveTextContent("ended Nov 13, 2099");
  });

  it("says voting ended on a proposal still marked voting past its end", async () => {
    const late = { ...page, items: [{ ...page.items[0], voting_end: "2000-01-01T00:00:00Z" }] };
    render({ daoProposals: () => Promise.resolve(late) });
    expect(await screen.findByTestId("dao-proposal-end")).toHaveTextContent("voting ended");
  });

  it("draws the live vote bar on voting rows only", async () => {
    render();
    const rows = await screen.findAllByTestId("dao-proposal-row");
    const bar = within(rows[0]).getByTestId("dao-proposal-live-bar");
    expect(within(bar).getAllByTestId("dao-tally-row").map((r) => r.textContent)).toEqual(["Yes42.0%", "No6.0%", "Abstain4.0%"]);
    expect(within(rows[1]).queryByTestId("dao-proposal-live-bar")).toBeNull();
  });

  it("asks for a status when its chip is chosen, and shows everything again for All", async () => {
    const daoProposals = vi.fn(() => Promise.resolve(page));
    render({ daoProposals });
    await screen.findAllByTestId("dao-proposal-row");
    expect(daoProposals).toHaveBeenCalledWith(2, { limit: 20, status: undefined }, expect.anything());
    await userEvent.click(screen.getByRole("button", { name: "Failed to execute" }));
    await waitFor(() => expect(daoProposals).toHaveBeenLastCalledWith(2, { limit: 20, status: "execution_failed" }, expect.anything()));
    await userEvent.click(screen.getByRole("button", { name: "All" }));
    expect(screen.getByRole("button", { name: "All" })).toHaveAttribute("aria-pressed", "true");
    expect(await screen.findAllByTestId("dao-proposal-row")).toHaveLength(3);
  });

  it("has a chip per status", async () => {
    render();
    await screen.findAllByTestId("dao-proposal-row");
    const chips = within(screen.getByRole("group", { name: "Filter by status" })).getAllByRole("button");
    expect(chips.map((c) => c.textContent)).toEqual(["All", "Voting", "Passed", "Failed to execute", "Rejected", "Expired", "Cancelled"]);
  });

  it("says a DAO has no proposals, and a status has none", async () => {
    render({ daoProposals: () => Promise.resolve({ items: [], next_cursor: null }) });
    expect(await screen.findByTestId("dao-proposals-empty")).toHaveTextContent("no proposals yet");
    await userEvent.click(screen.getByRole("button", { name: "Rejected" }));
    await waitFor(() => expect(screen.getByTestId("dao-proposals-empty")).toHaveTextContent("No proposals with this status."));
  });

  it("shows an error state with a way to retry", async () => {
    render({ daoProposals: () => Promise.reject(new ApiError(502, "upstream_error", "x")) });
    expect(await screen.findByTestId("dao-proposals-error")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("offers older proposals while a cursor is left, and appends them", async () => {
    const daoProposals = vi
      .fn()
      .mockResolvedValueOnce({ items: [page.items[0]], next_cursor: "c1" })
      .mockResolvedValue({ items: [page.items[1]], next_cursor: null });
    render({ daoProposals });
    await screen.findAllByTestId("dao-proposal-row");
    await userEvent.click(screen.getByRole("button", { name: "Show older proposals" }));
    await waitFor(() => expect(screen.getAllByTestId("dao-proposal-row")).toHaveLength(2));
    expect(daoProposals).toHaveBeenLastCalledWith(2, { cursor: "c1", limit: 20, status: undefined }, expect.anything());
    expect(screen.queryByRole("button", { name: "Show older proposals" })).toBeNull();
  });
});
