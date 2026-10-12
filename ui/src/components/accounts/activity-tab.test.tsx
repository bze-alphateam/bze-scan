import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ExplorerApi } from "@/api/client";
import type { Activity, ActivityParams, IbcTransfer, Page, TokenSummary } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import activity from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/activity.json";
import older from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/activity-older.json";
import ibc from "../../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/ibc.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";

import { ActivityTab } from "./activity-tab";

const ADDR = "bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4";
const first = activity as Page<Activity>;

function api(accountActivity: ExplorerApi["accountActivity"] = () => Promise.resolve(first)): ExplorerApi {
  return fakeApi({
    accountActivity,
    accountIbc: () => Promise.resolve(ibc as Page<IbcTransfer>),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
  });
}

const what = () => screen.getAllByTestId("activity-what").map((e) => e.textContent);

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

  it("asks for a category's rows, shows skeletons meanwhile and keeps the choice in the URL", async () => {
    let answer: (p: Page<Activity>) => void = () => {};
    const read = vi.fn((_: string, p: ActivityParams) =>
      p.category ? new Promise<Page<Activity>>((resolve) => (answer = resolve)) : Promise.resolve(first),
    );
    renderWithProviders(<ActivityTab address={ADDR} />, { api: api(read) });
    await screen.findAllByTestId("activity-row");

    await userEvent.click(screen.getByRole("button", { name: "DEX" }));
    expect(read).toHaveBeenLastCalledWith(ADDR, { limit: 25, category: "dex" }, expect.anything());
    expect(screen.getByRole("button", { name: "DEX" })).toHaveAttribute("aria-pressed", "true");
    expect(window.location.search).toBe("?tab=activity&category=dex");
    expect(screen.getAllByTestId("list-skeleton-row")).toHaveLength(8);

    answer({ items: first.items.filter((r) => r.category === "dex"), next_cursor: null });
    expect(await screen.findAllByTestId("activity-row")).toHaveLength(1);

    await userEvent.click(screen.getByRole("button", { name: "All" }));
    expect(window.location.search).toBe("");
    expect(await screen.findAllByTestId("activity-row")).toHaveLength(8);
  });

  it("opens on the category from the URL", async () => {
    const read = vi.fn(() => Promise.resolve({ items: [], next_cursor: null }));
    renderWithProviders(<ActivityTab address={ADDR} initialCategory="governance" />, { api: api(read) });
    expect(await screen.findByText("No activity in this category yet")).toBeInTheDocument();
    expect(read).toHaveBeenCalledWith(ADDR, { limit: 25, category: "governance" }, expect.anything());
    expect(screen.getByRole("button", { name: "Governance" })).toHaveAttribute("aria-pressed", "true");
  });

  it("appends older activity, with skeleton rows while it loads, and stops updating", async () => {
    let answer: (p: Page<Activity>) => void = () => {};
    const read = vi.fn((_: string, p: ActivityParams) =>
      p.cursor ? new Promise<Page<Activity>>((resolve) => (answer = resolve)) : Promise.resolve(first),
    );
    renderWithProviders(<ActivityTab address={ADDR} />, { api: api(read) });
    await screen.findAllByTestId("activity-row");

    await userEvent.click(screen.getByRole("button", { name: "Show older activity" }));
    expect(read).toHaveBeenLastCalledWith(ADDR, { cursor: first.next_cursor, limit: 25 }, expect.anything());
    expect(screen.getAllByTestId("list-skeleton-row")).toHaveLength(3);
    expect(screen.getAllByTestId("activity-row")).toHaveLength(8);

    answer(older as Page<Activity>);
    expect(await screen.findByText("Cross-chain transfer to Cosmos Hub")).toBeInTheDocument();
    expect(screen.getAllByTestId("activity-row")).toHaveLength(10);
    expect(screen.getByText("Delivered")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show older activity" })).not.toBeInTheDocument();
    expect(screen.getByTestId("activity-snapshot")).toHaveTextContent("the list stopped updating");
  });

  it("offers a retry when the list fails", async () => {
    const read = vi.fn(() => Promise.reject(new Error("down")));
    renderWithProviders(<ActivityTab address={ADDR} />, { api: api(read) });
    await userEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(read).toHaveBeenCalledTimes(2);
  });
});
