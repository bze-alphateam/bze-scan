import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { ChainParams, Page, Proposal, ProposalListParams, ProposalSummary } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import params from "../../../test/fixtures/api/v1/params.json";
import p48 from "../../../test/fixtures/api/v1/proposals/48.json";
import list from "../../../test/fixtures/api/v1/proposals.json";

import { ProposalsPage } from "./proposals-page";

const all = list as Page<ProposalSummary>;
const byStatus: ExplorerApi["proposals"] = (p) =>
  Promise.resolve({ items: all.items.filter((i) => !p.status || i.status === p.status), next_cursor: null });

function api(extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    proposals: byStatus,
    proposal: () => Promise.resolve(p48 as Proposal),
    params: () => Promise.resolve(params as ChainParams),
    ...extra,
  });
}

const titles = () => screen.getAllByTestId("proposal-row").map((r) => within(r).getByRole("link").textContent);

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"], now: new Date("2099-10-11T10:00:00Z") });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ProposalsPage", () => {
  it("shows one voting card and ten table rows as skeletons on the first load", () => {
    renderWithProviders(<ProposalsPage />, { api: pendingApi() });
    expect(screen.getAllByTestId("voting-card-skeleton")).toHaveLength(1);
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(10);
  });

  it("puts the live vote on top with its tally, turnout and verdict", async () => {
    renderWithProviders(<ProposalsPage />, { api: api() });
    const card = await screen.findByTestId("voting-card");
    expect(card).toHaveTextContent("#48 · Software upgrade");
    expect(within(card).getByRole("link", { name: "Upgrade network to v8.2.0" })).toHaveAttribute("href", "/proposals/48");
    expect(await within(card).findByText(/If passed, this proposal upgrades the chain to v8.2.0/)).toBeInTheDocument();
    expect(card).toHaveTextContent("byHOHoneycomb");
    expect(card).toHaveTextContent("ends Oct 14, 2099, 14:00:00 UTC (in 3 days 4 h)");
    expect(within(card).getAllByTestId("tally-row").map((r) => r.textContent)).toEqual([
      "Yes89.6%",
      "No6.0%",
      "Abstain3.0%",
      "Veto1.5%",
    ]);
    expect(await within(card).findByText("82.7% of staked BZE voted · quorum of 33.4% reached")).toBeInTheDocument();
    expect(within(card).getByTestId("verdict")).toHaveTextContent("On track to pass");
    expect(within(card).queryByTestId("historical-turnout")).toBeNull();
  });

  it("lists every proposal with its status, result, turnout and end", async () => {
    renderWithProviders(<ProposalsPage />, { api: api() });
    await screen.findByTestId("voting-card");
    expect(titles()).toEqual([
      "#49Fund the explorer's hosting for a year",
      "#48Upgrade network to v8.2.0",
      "#47Upgrade network to v8.1.1",
      "#46Change maximum number of validators to 100",
      "#45Fund Liquidity Pools",
      "#43Add ⚛️ Cosmos Ecosystem ⚛️  as Publisher",
    ]);
    const [p49, p48row, , p46, p45row, p43row] = screen.getAllByTestId("proposal-row");
    expect(p49).toHaveTextContent("Deposit period");
    expect(p48row).toHaveTextContent("Voting now");
    expect(p48row).toHaveTextContent("Yes 89.6%");
    expect(p48row).toHaveTextContent("ends in 4 days");
    expect(p46).toHaveTextContent("Rejected");
    expect(p46).toHaveTextContent("No 70.6%");
    expect(p46).toHaveTextContent("May 7, 2026");

    // Only the turnout the archive measured is marked, in its own column.
    expect(screen.getAllByTestId("historical-turnout")).toHaveLength(1);
    const chip = within(within(p43row).getByTestId("turnout")).getByTestId("historical-turnout");
    expect(chip).toHaveTextContent("historical");
    expect(chip).toHaveAttribute("title", expect.stringContaining("the explorer was not running when this proposal was voted on"));
    expect(within(p43row).getByTestId("turnout")).toHaveTextContent("67.3%");
    expect(within(p45row).queryByTestId("historical-turnout")).toBeNull();
    expect(within(p49).queryByTestId("historical-turnout")).toBeNull();
  });

  it("marks a voting card's turnout only when the archive measured it", async () => {
    const archived = { ...all.items[1], tally_source: "archive" } as ProposalSummary;
    const proposals = vi.fn(() => Promise.resolve({ items: [archived], next_cursor: null }));
    renderWithProviders(<ProposalsPage />, { api: api({ proposals }) });
    const card = await screen.findByTestId("voting-card");
    expect(within(card).getByTestId("historical-turnout")).toHaveTextContent("historical");
  });

  it("filters by chip on the API and by title on the page", async () => {
    const proposals = vi.fn(byStatus);
    renderWithProviders(<ProposalsPage />, { api: api({ proposals }) });
    await screen.findByTestId("voting-card");
    await userEvent.click(screen.getByRole("button", { name: "Passed" }));
    expect(screen.getByRole("button", { name: "Passed" })).toHaveAttribute("aria-pressed", "true");
    await waitFor(() => expect(titles()).toEqual(["#47Upgrade network to v8.1.1", "#45Fund Liquidity Pools", "#43Add ⚛️ Cosmos Ecosystem ⚛️  as Publisher"]));
    expect(proposals).toHaveBeenCalledWith({ limit: 20, status: "passed" }, expect.anything());
    await userEvent.type(screen.getByRole("textbox", { name: "Find a proposal" }), "liquidity");
    expect(titles()).toEqual(["#45Fund Liquidity Pools"]);
    await userEvent.click(screen.getByRole("button", { name: "Rejected" }));
    expect(await screen.findByText("No proposal has that title.")).toBeInTheDocument();
  });

  it("shows skeleton rows while a chip loads a different list", async () => {
    let release: (v: Page<ProposalSummary>) => void = () => {};
    const proposals = vi.fn((p: ProposalListParams) =>
      p.status === "rejected" ? new Promise<Page<ProposalSummary>>((r) => (release = r)) : byStatus(p),
    );
    renderWithProviders(<ProposalsPage />, { api: api({ proposals }) });
    await screen.findByTestId("voting-card");
    await userEvent.click(screen.getByRole("button", { name: "Rejected" }));
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(10);
    release({ items: [all.items[3]], next_cursor: null });
    await waitFor(() => expect(titles()).toEqual(["#46Change maximum number of validators to 100"]));
  });

  it("follows next_cursor for older proposals", async () => {
    const proposals = vi.fn((p: ProposalListParams) =>
      Promise.resolve(
        p.status
          ? { items: [], next_cursor: null }
          : p.cursor
            ? { items: all.items.slice(3), next_cursor: null }
            : { items: all.items.slice(0, 3), next_cursor: "c1" },
      ),
    );
    renderWithProviders(<ProposalsPage />, { api: api({ proposals }) });
    await waitFor(() => expect(screen.getAllByTestId("proposal-row")).toHaveLength(3));
    await userEvent.click(screen.getByRole("button", { name: "Show older proposals" }));
    await waitFor(() => expect(screen.getAllByTestId("proposal-row")).toHaveLength(all.items.length));
    expect(proposals).toHaveBeenCalledWith({ cursor: "c1", limit: 20, status: undefined }, expect.anything());
  });

  it("offers a retry when the API fails", async () => {
    const proposals = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<ProposalsPage />, { api: api({ proposals }) });
    await userEvent.click(await screen.findByRole("button", { name: "Try again" }));
    await waitFor(() => expect(proposals.mock.calls.length).toBeGreaterThanOrEqual(3));
  });
});
