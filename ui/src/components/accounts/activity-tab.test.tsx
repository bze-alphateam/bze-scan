import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ExplorerApi } from "@/api/client";
import type { Activity, ActivityPage, ActivityParams, IbcTransfer, Page, Status, TokenSummary } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import activity from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/activity.json";
import ibc from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/ibc.json";
import status from "../../../test/fixtures/api/v1/status.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";

import { ActivityTab } from "./activity-tab";

const ADDR = "bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4";
const rows = (activity as Page<Activity>).items;
const TOTAL = 1530;

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

/** The API's numbered page: the fixture's rows (of the category) on every page up to the last, the total alongside. */
const pageOf = (p: ActivityParams, total = TOTAL): ActivityPage => {
  const items = p.category ? rows.filter((r) => r.category === p.category) : rows;
  const all = p.category ? items.length : total;
  return { items: (p.page ?? 1) <= Math.ceil(all / 25) ? items : [], next_cursor: null, total: all };
};

function api(accountActivity: ExplorerApi["accountActivity"] = (_, p) => Promise.resolve(pageOf(p))): ExplorerApi {
  return fakeApi({
    accountActivity,
    accountIbc: () => Promise.resolve(ibc as Page<IbcTransfer>),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    status: () => Promise.resolve(status as unknown as Status),
  });
}

const what = () => screen.getAllByTestId("activity-what").map((e) => e.textContent);
const range = () => screen.getByTestId("activity-range").textContent;
const pageCalls = (read: ReturnType<typeof vi.fn>) => read.mock.calls.map((c) => c[1] as ActivityParams);

beforeEach(() => {
  window.history.replaceState(null, "", `/accounts/${ADDR}`);
});

describe("ActivityTab", () => {
  it("shows eight skeleton rows on the first load", () => {
    renderWithProviders(<ActivityTab address={ADDR} />, { api: pendingApi() });
    expect(screen.getAllByTestId("list-skeleton-row")).toHaveLength(8);
    expect(screen.getAllByRole("button", { name: /^(All|DEX|Cross-chain)$/ })).toHaveLength(3);
  });

  it("renders a row per transaction or block in plain words, with chips, times and amounts", async () => {
    renderWithProviders(<ActivityTab address={ADDR} />, { api: api() });
    await screen.findAllByTestId("activity-row");
    expect(what()).toEqual([
      // Avatar initials are part of a chip's text.
      "Sent to 8Ubze18uf…9zhw",
      "Order filled · sold 200 BZE for 2.56 USDC on BZE/USDC",
      "Received from Q8bze1q8f…x0rq",
      "Staked with HOHoneycomb",
      "Funded the Burner",
      "Claimed rewards from 2 validators",
      "Cross-chain transfer from NobleReceived",
      "SendFailed",
    ]);
    const rows = screen.getAllByTestId("activity-row");

    // The counterparty is a chip linking to its page.
    expect(within(rows[0]).getByRole("link", { name: "bze18uf…9zhw" })).toHaveAttribute("href", "/accounts/bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw");
    expect(within(rows[3]).getByRole("link", { name: "Honeycomb" })).toHaveAttribute(
      "href",
      "/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c",
    );

    // Amounts: signed, the fee apart, "—" when nothing moved.
    const amounts = rows.map((r) => within(r).getByTestId("activity-amounts").textContent);
    expect(amounts[0]).toBe("− 10 BZE− 0.002 BZE fee");
    expect(amounts[1]).toBe("+ 2.56 USDC");
    expect(amounts[5]).toBe("+ 15.75 BZE− 0.002 BZE fee");
    expect(amounts[7]).toBe("− 0.002 BZE fee");

    // A chain-made row has no transaction of the address; a failed one says so.
    expect(within(rows[1]).getByTestId("activity-when")).toHaveTextContent("settled in block 25,120,301, no transaction of yours");
    expect(within(rows[1]).getByRole("link", { name: "25,120,301" })).toHaveAttribute("href", "/blocks/25120301");
    expect(within(rows[0]).getByRole("link", { name: /^1111/ })).toHaveAttribute("href", `/txs/${"1".repeat(64)}`);
    expect(within(rows[7]).getByTestId("activity-failed")).toHaveTextContent("Failed · see the transaction for the reason");
    expect(within(rows[0]).queryByTestId("activity-failed")).not.toBeInTheDocument();
  });

  it("asks for page 1 of 25 rows and numbers the pages from the total", async () => {
    const read = vi.fn((_: string, p: ActivityParams) => Promise.resolve(pageOf(p)));
    renderWithProviders(<ActivityTab address={ADDR} />, { api: api(read) });
    await screen.findAllByTestId("activity-row");
    expect(read).toHaveBeenCalledWith(ADDR, { page: 1, limit: 25 }, expect.anything());
    expect(range()).toBe("Activity 1–25 of 1,530");
    const nav = screen.getByRole("navigation", { name: "Pagination" });
    // First, last and the current ±1, with a gap.
    expect(within(nav).getByTestId("pager-current")).toHaveTextContent("1");
    expect(within(nav).getByRole("link", { name: "Page 2" })).toHaveAttribute("href", `/accounts/${ADDR}?tab=activity&page=2`);
    expect(within(nav).getByRole("link", { name: "Page 62" })).toBeInTheDocument();
    expect(within(nav).getByText("…")).toBeInTheDocument();
    expect(within(nav).getByRole("link", { name: "Last page" })).toHaveAttribute("href", `/accounts/${ADDR}?tab=activity&page=62`);
  });

  it("moves to page n asking for page=n, keeping it in the URL", async () => {
    const read = vi.fn((_: string, p: ActivityParams) => Promise.resolve(pageOf(p)));
    renderWithProviders(<ActivityTab address={ADDR} />, { api: api(read) });
    await screen.findAllByTestId("activity-row");

    await userEvent.click(screen.getByRole("link", { name: "Page 2" }));
    expect(read).toHaveBeenCalledWith(ADDR, { page: 2, limit: 25 }, expect.anything());
    expect(window.location.search).toBe("?tab=activity&page=2");
    await screen.findByText("Activity 26–50 of 1,530");

    await userEvent.click(screen.getByRole("link", { name: "Page 3" }));
    await screen.findByText("Activity 51–75 of 1,530");
    expect(screen.getByTestId("pager-current")).toHaveTextContent("3");
    expect(within(screen.getByRole("navigation", { name: "Pagination" })).getAllByText("…")).toHaveLength(1);

    await userEvent.click(screen.getByRole("link", { name: "First page" }));
    await screen.findByText("Activity 1–25 of 1,530");
    expect(window.location.search).toBe("");
  });

  it("goes to the last page, where there is no next one", async () => {
    renderWithProviders(<ActivityTab address={ADDR} />, { api: api() });
    await screen.findAllByTestId("activity-row");
    await userEvent.click(screen.getByRole("link", { name: "Last page" }));
    await screen.findByText("Activity 1,526–1,530 of 1,530");
    expect(window.location.search).toBe("?tab=activity&page=62");
    expect(screen.getByLabelText("Next page")).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByLabelText("Last page")).toHaveAttribute("aria-disabled", "true");
  });

  it("clamps a typed page to the last one", async () => {
    const read = vi.fn((_: string, p: ActivityParams) => Promise.resolve(pageOf(p)));
    renderWithProviders(<ActivityTab address={ADDR} />, { api: api(read) });
    await screen.findAllByTestId("activity-row");
    await userEvent.type(screen.getByLabelText("Page"), "999{Enter}");
    expect(read).toHaveBeenCalledWith(ADDR, { page: 62, limit: 25 }, expect.anything());
    await screen.findByText("Activity 1,526–1,530 of 1,530");
  });

  it("goes back to page 1 when the category changes, keeping both in the URL", async () => {
    const read = vi.fn((_: string, p: ActivityParams) => Promise.resolve(pageOf(p)));
    renderWithProviders(<ActivityTab address={ADDR} initialPage={3} />, { api: api(read) });
    await screen.findByText("Activity 51–75 of 1,530");

    await userEvent.click(screen.getByRole("button", { name: "DEX" }));
    expect(read).toHaveBeenLastCalledWith(ADDR, { page: 1, limit: 25, category: "dex" }, expect.anything());
    expect(screen.getByRole("button", { name: "DEX" })).toHaveAttribute("aria-pressed", "true");
    expect(window.location.search).toBe("?tab=activity&category=dex");
    expect(await screen.findAllByTestId("activity-row")).toHaveLength(1);
    expect(range()).toBe("Activity 1–1 of 1");

    await userEvent.click(screen.getByRole("button", { name: "All" }));
    expect(window.location.search).toBe("");
    expect(await screen.findAllByTestId("activity-row")).toHaveLength(8);
  });

  it("opens on the category and page from the URL", async () => {
    const read = vi.fn((_: string, p: ActivityParams) => Promise.resolve(pageOf(p)));
    renderWithProviders(<ActivityTab address={ADDR} initialCategory="staking" initialPage={1} />, { api: api(read) });
    expect(await screen.findAllByTestId("activity-row")).toHaveLength(2);
    expect(read).toHaveBeenCalledWith(ADDR, { page: 1, limit: 25, category: "staking" }, expect.anything());
    expect(screen.getByRole("button", { name: "Staking" })).toHaveAttribute("aria-pressed", "true");
  });

  it("says when a category has no activity yet", async () => {
    renderWithProviders(<ActivityTab address={ADDR} initialCategory="governance" />, { api: api() });
    expect(await screen.findByText("No activity in this category yet")).toBeInTheDocument();
    expect(screen.queryByTestId("pager")).not.toBeInTheDocument();
  });

  it("polls only page 1; on an older page a pill counts what came since and goes back to the latest", async () => {
    // The older page was read at 1,530 rows; the live total is now 1,533.
    const read = vi.fn((_: string, p: ActivityParams) => Promise.resolve(pageOf(p, p.page === 1 ? 1533 : TOTAL)));
    renderWithProviders(<ActivityTab address={ADDR} initialPage={2} />, { api: api(read) });
    const pill = await screen.findByTestId("activity-new");
    expect(pill).toHaveTextContent("3 new · back to latest");
    // The total alone: one row of page 1.
    expect(pageCalls(read)).toContainEqual({ page: 1, limit: 1 });

    await userEvent.click(pill);
    await screen.findByText("Activity 1–25 of 1,533");
    expect(window.location.search).toBe("");
    expect(screen.queryByTestId("activity-new")).not.toBeInTheDocument();
  });

  it("shows no pill on page 1, and no pill when nothing came", async () => {
    const read = vi.fn((_: string, p: ActivityParams) => Promise.resolve(pageOf(p)));
    renderWithProviders(<ActivityTab address={ADDR} initialPage={2} />, { api: api(read) });
    await screen.findByText("Activity 26–50 of 1,530");
    await vi.waitFor(() => expect(pageCalls(read)).toContainEqual({ page: 1, limit: 1 }));
    expect(screen.queryByTestId("activity-new")).not.toBeInTheDocument();
  });

  it("says when an address was never seen doing anything, with the history's start", async () => {
    const read = vi.fn();
    renderWithProviders(<ActivityTab address={ADDR} neverSeen />, { api: api(read) });
    await screen.findByText("24,998,316");
    expect(screen.getByTestId("activity-never")).toHaveTextContent(
      "No activity seen yetNothing this account did is in the explorer's history (from block 24,998,316). Balances above are read live from the chain, so they are right even when the history is empty.",
    );
    expect(read).not.toHaveBeenCalled();
    expect(screen.queryByRole("group", { name: "Show activity" })).not.toBeInTheDocument();
  });

  it("offers a retry when the list fails", async () => {
    const read = vi.fn(() => Promise.reject(new Error("down")));
    renderWithProviders(<ActivityTab address={ADDR} />, { api: api(read) });
    await userEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(read).toHaveBeenCalledTimes(2);
  });
});
