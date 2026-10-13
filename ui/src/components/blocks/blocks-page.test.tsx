import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { BlockListParams, BlockSummary, Page, Stats, Status } from "@/api/types";
import { pendingApi, fakeApi, renderWithProviders } from "@/test/render";

import blocks from "../../../test/fixtures/api/v1/blocks.json";
import stats from "../../../test/fixtures/api/v1/stats.json";
import status from "../../../test/fixtures/api/v1/status.json";

import { BlocksPage } from "./blocks-page";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));

const latest = blocks as Page<BlockSummary>;

/** `n` blocks counting down from `from`, made from the fixture's rows. */
const rows = (from: number, n: number): Page<BlockSummary> => ({
  items: Array.from({ length: n }, (_, i) => ({ ...latest.items[i % latest.items.length], height: from - i })),
  next_cursor: null,
});

/** A status whose history starts at `oldest`, a stats whose latest block is `tip`. */
function api({ oldest = 1, tip = 1000, blocks: read }: { oldest?: number; tip?: number; blocks: ExplorerApi["blocks"] }): ExplorerApi {
  const s = status as Status;
  return fakeApi({
    blocks: read,
    status: () => Promise.resolve({ ...s, back_fill: { ...s.back_fill, oldest_height: oldest } }),
    stats: () => Promise.resolve({ ...(stats as unknown as Stats), latest_height: tip }),
  });
}

const pinnedAnswer = (p: BlockListParams) => Promise.resolve(rows((p.before ?? 1001) - 1, 25));

const link = (name: string) => screen.getByRole("link", { name });

beforeEach(() => {
  push.mockClear();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("BlocksPage", () => {
  it("shows ten skeleton rows on the first load", () => {
    renderWithProviders(<BlocksPage top={null} page={1} />, { api: pendingApi() });
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(10);
  });

  it("lists the blocks with proposer, transactions, block time and size", async () => {
    renderWithProviders(<BlocksPage top={null} page={1} />, { api: api({ blocks: () => Promise.resolve(latest) }) });
    const list = await screen.findAllByTestId("block-row");
    expect(list).toHaveLength(10);
    expect(within(list[0]).getByRole("link", { name: "25,120,449" })).toHaveAttribute("href", "/blocks/25120449");
    expect(within(list[0]).getByRole("link", { name: "Honeycomb" })).toBeInTheDocument();
    expect(list[1]).toHaveTextContent("—");

    const last = list[9];
    expect(within(last).getByTestId("block-txs")).toHaveTextContent("7 · 1 failed");
    expect(last).toHaveTextContent("5.8 s");
    expect(last).toHaveTextContent("4 KB");
    expect(within(list[1]).getByTestId("block-txs")).toHaveTextContent(/^0$/);
  });

  it("polls the live page, which has no top, and pins every link away from it to the newest height seen", async () => {
    vi.useFakeTimers();
    const read = vi.fn(() => Promise.resolve(rows(1000, 25)));
    renderWithProviders(<BlocksPage top={null} page={1} />, { api: api({ oldest: 1, blocks: read }) });
    await act(() => vi.advanceTimersByTimeAsync(5));
    expect(read).toHaveBeenLastCalledWith({ limit: 25 }, expect.anything());
    expect(screen.getByTestId("blocks-range")).toHaveTextContent("Blocks 1,000 – 976");
    expect(screen.getByTestId("pager-current")).toHaveTextContent("1");
    expect(link("Next page")).toHaveAttribute("href", "/blocks?top=1000&page=2");
    expect(link("Last page")).toHaveAttribute("href", "/blocks?top=1000&page=40");
    expect(screen.queryByTestId("blocks-new")).not.toBeInTheDocument();

    read.mockImplementation(() => Promise.resolve(rows(1002, 25)));
    await act(() => vi.advanceTimersByTimeAsync(7_005));
    expect(read).toHaveBeenCalledTimes(2);
    expect(link("Next page")).toHaveAttribute("href", "/blocks?top=1002&page=2");
  });

  it("asks page p for the heights below top−(p−1)·25+1 and never polls it", async () => {
    vi.useFakeTimers();
    const read = vi.fn(pinnedAnswer);
    renderWithProviders(<BlocksPage top={1000} page={3} />, { api: api({ blocks: read }) });
    await act(() => vi.advanceTimersByTimeAsync(5));
    expect(read).toHaveBeenCalledWith({ before: 951, limit: 25 }, expect.anything());
    expect(screen.getByTestId("blocks-range")).toHaveTextContent("Blocks 950 – 926");
    expect(link("First page")).toHaveAttribute("href", "/blocks");
    expect(link("Previous page")).toHaveAttribute("href", "/blocks?top=1000&page=2");
    expect(link("Next page")).toHaveAttribute("href", "/blocks?top=1000&page=4");

    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(read).toHaveBeenCalledTimes(1);
  });

  it("keeps page 1 pinned when it has a top, its previous page link included", async () => {
    renderWithProviders(<BlocksPage top={1000} page={2} />, { api: api({ blocks: pinnedAnswer }) });
    expect(await screen.findByRole("link", { name: "Previous page" })).toHaveAttribute("href", "/blocks?top=1000&page=1");
  });

  it("counts the last page from the history start and disables the ends", async () => {
    renderWithProviders(<BlocksPage top={1000} page={2} />, { api: api({ oldest: 951, blocks: pinnedAnswer }) });
    await screen.findAllByTestId("block-row");
    for (const name of ["Next page", "Last page"]) {
      expect(screen.getByRole("link", { name })).toHaveAttribute("aria-disabled", "true");
      expect(screen.getByRole("link", { name })).not.toHaveAttribute("href");
    }
    expect(link("Previous page")).not.toHaveAttribute("aria-disabled");
  });

  it("disables « and ‹ on page 1", async () => {
    renderWithProviders(<BlocksPage top={null} page={1} />, { api: api({ blocks: () => Promise.resolve(rows(1000, 25)) }) });
    await screen.findAllByTestId("block-row");
    for (const name of ["First page", "Previous page"]) {
      expect(screen.getByRole("link", { name })).toHaveAttribute("aria-disabled", "true");
    }
  });

  it("clamps a typed page to 1…last", async () => {
    renderWithProviders(<BlocksPage top={1000} page={2} />, { api: api({ blocks: pinnedAnswer }) });
    await screen.findAllByTestId("block-row");
    const box = screen.getByRole("textbox", { name: "Page" });

    fireEvent.change(box, { target: { value: "99" } });
    fireEvent.click(screen.getByRole("button", { name: "Go to page" }));
    expect(push).toHaveBeenLastCalledWith("/blocks?top=1000&page=40");

    fireEvent.change(box, { target: { value: "0" } });
    fireEvent.click(screen.getByRole("button", { name: "Go to page" }));
    expect(push).toHaveBeenLastCalledWith("/blocks?top=1000&page=1");

    fireEvent.change(box, { target: { value: "7" } });
    fireEvent.submit(box);
    expect(push).toHaveBeenLastCalledWith("/blocks?top=1000&page=7");
  });

  it("stays live when page 1 is typed on the live page", async () => {
    renderWithProviders(<BlocksPage top={null} page={1} />, { api: api({ blocks: () => Promise.resolve(rows(1000, 25)) }) });
    await screen.findAllByTestId("block-row");
    fireEvent.change(screen.getByRole("textbox", { name: "Page" }), { target: { value: "1" } });
    fireEvent.click(screen.getByRole("button", { name: "Go to page" }));
    expect(push).toHaveBeenLastCalledWith("/blocks");
  });

  it("counts the blocks produced since the pin and links back to the latest", async () => {
    renderWithProviders(<BlocksPage top={1000} page={1} />, { api: api({ tip: 1003, blocks: pinnedAnswer }) });
    const pill = await screen.findByTestId("blocks-new");
    expect(pill).toHaveTextContent("3 new blocks · back to latest");
    expect(pill).toHaveAttribute("href", "/blocks");
  });

  it("shows a short page as it came back", async () => {
    renderWithProviders(<BlocksPage top={1000} page={2} />, { api: api({ blocks: () => Promise.resolve(rows(975, 3)) }) });
    expect(await screen.findAllByTestId("block-row")).toHaveLength(3);
    expect(screen.getByTestId("blocks-range")).toHaveTextContent("Blocks 975 – 973");
    expect(link("Next page")).toHaveAttribute("href", "/blocks?top=1000&page=3");
  });

  it("offers a retry when the API fails", async () => {
    const fail = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<BlocksPage top={null} page={1} />, { api: api({ blocks: fail }) });
    expect(await screen.findByRole("heading", { name: "The blocks could not be loaded" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await vi.waitFor(() => expect(fail).toHaveBeenCalledTimes(2));
  });
});
