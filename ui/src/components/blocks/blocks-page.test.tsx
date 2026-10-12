import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import type { BlockSummary, Page, PageParams } from "@/api/types";
import { pendingApi, fakeApi, renderWithProviders } from "@/test/render";

import blocks from "../../../test/fixtures/api/v1/blocks.json";

import { BlocksPage } from "./blocks-page";

const latest = blocks as Page<BlockSummary>;
const older: Page<BlockSummary> = { items: [{ ...latest.items[9], height: 25120439, time: "2026-10-11T09:14:56Z" }], next_cursor: null };

afterEach(() => {
  vi.useRealTimers();
});

const answer = (p: PageParams) => Promise.resolve(p.cursor ? older : latest);

describe("BlocksPage", () => {
  it("shows ten skeleton rows on the first load", () => {
    renderWithProviders(<BlocksPage />, { api: pendingApi() });
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(10);
  });

  it("lists the blocks with proposer, transactions, block time and size", async () => {
    renderWithProviders(<BlocksPage />, { api: fakeApi({ blocks: answer }) });
    const rows = await screen.findAllByTestId("block-row");
    expect(rows).toHaveLength(10);
    expect(within(rows[0]).getByRole("link", { name: "25,120,449" })).toHaveAttribute("href", "/blocks/25120449");
    expect(within(rows[0]).getByRole("link", { name: "Honeycomb" })).toBeInTheDocument();
    expect(within(rows[1]).queryByRole("link", { name: "Honeycomb" })).not.toBeInTheDocument();
    expect(rows[1]).toHaveTextContent("—");

    const last = rows[9];
    expect(within(last).getByTestId("block-txs")).toHaveTextContent("7 · 1 failed");
    expect(within(last).getByText("· 1 failed")).toBeInTheDocument();
    expect(last).toHaveTextContent("5.8 s");
    expect(last).toHaveTextContent("4 KB");
    expect(within(rows[1]).getByTestId("block-txs")).toHaveTextContent(/^Transactions0$/);
  });

  it("polls the first page, stops once an older page is loaded, and goes back to the latest", async () => {
    vi.useFakeTimers();
    const fetchBlocks = vi.fn(answer);
    renderWithProviders(<BlocksPage />, { api: fakeApi({ blocks: fetchBlocks }) });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getAllByTestId("block-row")).toHaveLength(10);
    expect(fetchBlocks).toHaveBeenCalledTimes(1);

    await act(() => vi.advanceTimersByTimeAsync(7_000));
    expect(fetchBlocks).toHaveBeenCalledTimes(2);

    fireEvent.click(screen.getByRole("button", { name: "Show older blocks" }));
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(fetchBlocks).toHaveBeenLastCalledWith({ cursor: "25120439", limit: 25 }, expect.anything());
    expect(screen.getAllByTestId("block-row")).toHaveLength(11);
    expect(screen.getByTestId("blocks-snapshot")).toHaveTextContent("the list stopped updating");
    expect(screen.queryByRole("button", { name: "Show older blocks" })).not.toBeInTheDocument();
    const calls = fetchBlocks.mock.calls.length;

    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(fetchBlocks).toHaveBeenCalledTimes(calls);

    vi.spyOn(window, "scrollTo").mockImplementation(() => {});
    fireEvent.click(screen.getByRole("button", { name: "Back to the latest blocks" }));
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(fetchBlocks).toHaveBeenCalledTimes(calls + 1);
    expect(fetchBlocks).toHaveBeenLastCalledWith({ limit: 25 }, expect.anything());
    expect(screen.getAllByTestId("block-row")).toHaveLength(10);
    expect(screen.queryByTestId("blocks-snapshot")).not.toBeInTheDocument();

    await act(() => vi.advanceTimersByTimeAsync(7_000));
    expect(fetchBlocks).toHaveBeenCalledTimes(calls + 2);
  });

  it("offers a retry when the API fails", async () => {
    const fail = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<BlocksPage />, { api: fakeApi({ blocks: fail }) });
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    await vi.waitFor(() => expect(fail).toHaveBeenCalledTimes(2));
  });
});
