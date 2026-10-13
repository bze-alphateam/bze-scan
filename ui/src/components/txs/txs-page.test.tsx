import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import type { Page, TokenSummary, TxListParams, TxSummary, ValidatorList } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import tokens from "../../../test/fixtures/api/v1/tokens.json";
import txs from "../../../test/fixtures/api/v1/txs.json";
import validators from "../../../test/fixtures/api/v1/validators.json";

import { TxsPage } from "./txs-page";

const latest = txs as Page<TxSummary>;
const older: Page<TxSummary> = { items: [{ ...latest.items[9], height: 25120439, hash: "A".repeat(64) }], next_cursor: null };
const failed: Page<TxSummary> = { items: latest.items.filter((t) => !t.success), next_cursor: null };

const answer = (p: TxListParams) => Promise.resolve(p.cursor ? older : p.status === "failed" ? failed : latest);

/** A chain of single-transaction blocks from `tip` down to 1; a cursor "cH" asks for the ones below H. */
function makeChain(tip: number) {
  const chain = {
    tip,
    read: vi.fn((p: TxListParams): Promise<Page<TxSummary>> => {
      const from = p.cursor ? Number(p.cursor.slice(1)) - 1 : chain.tip;
      const items = range(from, Math.min(25, from)).map((h) => ({ ...latest.items[0], height: h, tx_index: 0, hash: h.toString(16).padStart(64, "0") }));
      const last = items.at(-1)?.height ?? 1;
      return Promise.resolve({ items, next_cursor: last > 1 ? `c${last}` : null });
    }),
  };
  return chain;
}

const range = (from: number, n: number) => Array.from({ length: n }, (_, i) => from - i);
const heights = () => screen.getAllByTestId("tx-height").map((e) => Number(e.textContent!.replace(/\D/g, "")));
const current = () => screen.getByTestId("pager-current");
const button = (name: string) => screen.getByRole("button", { name });
// React Query notifies on a 0 ms timer after an answer.
const settle = () => act(() => vi.advanceTimersByTimeAsync(5));

function api(transactions: (p: TxListParams) => Promise<Page<TxSummary>> = answer) {
  return fakeApi({
    transactions,
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    validators: () => Promise.resolve(validators as ValidatorList),
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe("TxsPage", () => {
  it("shows ten skeleton rows on the first load", () => {
    renderWithProviders(<TxsPage />, { api: pendingApi() });
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(10);
  });

  it("lists each transaction in one line with signer, status, fee, time and height", async () => {
    renderWithProviders(<TxsPage />, { api: api() });
    const rows = await screen.findAllByTestId("tx-row");
    expect(rows).toHaveLength(10);
    expect(within(rows[0]).getByTestId("tx-what")).toHaveTextContent("Send 25 BZE to bze1m3k…9ds4");
    expect(within(rows[0]).getByRole("link", { name: "27CE…916B" })).toHaveAttribute("href", `/txs/${latest.items[0].hash}`);
    expect(within(rows[0]).getByRole("link", { name: "bze1q8f…x0rq" })).toBeInTheDocument();
    expect(within(rows[0]).getByTestId("tx-height")).toHaveTextContent("block 25,120,449");
    expect(rows[0]).toHaveTextContent("0.002 BZE");
    expect(within(rows[1]).getByTestId("tx-what")).toHaveTextContent("Stake 500 BZE with Honeycomb");
    expect(within(rows[6]).getByText("Failed")).toBeInTheDocument();
    expect(within(rows[7]).getByTestId("tx-what")).toHaveTextContent(/\+ 1 more action$/);
  });

  it("asks for the status of the chip, with skeleton rows while the new list loads", async () => {
    let release: () => void = () => {};
    const fetchTxs = vi.fn((p: TxListParams) =>
      p.status ? new Promise<Page<TxSummary>>((resolve) => (release = () => resolve(failed))) : answer(p),
    );
    renderWithProviders(<TxsPage />, { api: api(fetchTxs) });
    await screen.findAllByTestId("tx-row");

    fireEvent.click(screen.getByRole("button", { name: "Failed" }));
    expect(screen.getByRole("button", { name: "Failed" })).toHaveAttribute("aria-pressed", "true");
    expect(fetchTxs).toHaveBeenLastCalledWith({ limit: 25, status: "failed" }, expect.anything());
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(10);
    await act(async () => release());
    expect(await screen.findAllByTestId("tx-row")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "Success" }));
    expect(fetchTxs).toHaveBeenLastCalledWith({ limit: 25, status: "success" }, expect.anything());
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    expect(await screen.findAllByTestId("tx-row")).toHaveLength(10);
  });

  it("pins the list on next: asks for the page after the last row and stops refreshing it", async () => {
    vi.useFakeTimers();
    const chain = makeChain(1000);
    renderWithProviders(<TxsPage />, { api: api(chain.read) });
    await settle();
    expect(heights()).toEqual(range(1000, 25));
    expect(current()).toHaveTextContent("1");

    fireEvent.click(button("Next page"));
    await settle();
    expect(chain.read).toHaveBeenLastCalledWith({ limit: 25, cursor: "c976" }, expect.anything());
    expect(current()).toHaveTextContent("2");
    expect(heights()).toEqual(range(975, 25));

    // The live page moves on; the pinned page does not, the pill counts what is newer.
    chain.tip = 1003;
    await act(() => vi.advanceTimersByTimeAsync(7_005));
    expect(heights()[0]).toBe(975);
    expect(screen.getByTestId("txs-new")).toHaveTextContent("3 new transactions · back to latest");
    const pinned = chain.read.mock.calls.filter(([p]) => p.cursor === "c976");
    expect(pinned).toHaveLength(1);
  });

  it("pops back to the exact previous page and resets to the live page with «", async () => {
    vi.useFakeTimers();
    const chain = makeChain(1000);
    renderWithProviders(<TxsPage />, { api: api(chain.read) });
    await settle();
    expect(button("Newest page")).toBeDisabled();
    expect(button("Previous page")).toBeDisabled();

    fireEvent.click(button("Next page"));
    await settle();
    // New transactions arrive while paging: the pages keep hanging from the first top.
    chain.tip = 1010;
    fireEvent.click(button("Next page"));
    await settle();
    expect(chain.read).toHaveBeenLastCalledWith({ limit: 25, cursor: "c951" }, expect.anything());
    expect(current()).toHaveTextContent("3");
    expect(heights()[0]).toBe(950);

    fireEvent.click(button("Previous page"));
    await settle();
    expect(current()).toHaveTextContent("2");
    expect(heights()[0]).toBe(975);

    fireEvent.click(button("Next page"));
    await settle();
    fireEvent.click(button("Newest page"));
    await settle();
    expect(current()).toHaveTextContent("1");
    expect(heights()[0]).toBe(1010);
    expect(screen.queryByTestId("txs-new")).not.toBeInTheDocument();
  });

  it("returns to the live page from the pill and from the status pills", async () => {
    vi.useFakeTimers();
    const chain = makeChain(1000);
    renderWithProviders(<TxsPage />, { api: api(chain.read) });
    await settle();
    fireEvent.click(button("Next page"));
    await settle();
    chain.tip = 1001;
    await act(() => vi.advanceTimersByTimeAsync(7_005));
    expect(screen.getByTestId("txs-new")).toHaveTextContent("1 new transaction · back to latest");
    fireEvent.click(screen.getByTestId("txs-new"));
    await settle();
    expect(current()).toHaveTextContent("1");
    expect(heights()[0]).toBe(1001);

    fireEvent.click(button("Next page"));
    await settle();
    fireEvent.click(button("Failed"));
    await settle();
    expect(current()).toHaveTextContent("1");
    expect(chain.read).toHaveBeenLastCalledWith({ limit: 25, status: "failed" }, expect.anything());
  });

  it("counts no news as no pill, and a whole newer first page as 25+", async () => {
    vi.useFakeTimers();
    const chain = makeChain(1000);
    renderWithProviders(<TxsPage />, { api: api(chain.read) });
    await settle();
    fireEvent.click(button("Next page"));
    await settle();
    await act(() => vi.advanceTimersByTimeAsync(7_005));
    expect(screen.queryByTestId("txs-new")).not.toBeInTheDocument();

    chain.tip = 1100;
    await act(() => vi.advanceTimersByTimeAsync(7_005));
    expect(screen.getByTestId("txs-new")).toHaveTextContent("25+ new transactions · back to latest");
  });

  it("disables next on the last page", async () => {
    renderWithProviders(<TxsPage />, { api: api(() => Promise.resolve({ items: latest.items, next_cursor: null })) });
    await screen.findAllByTestId("tx-row");
    expect(button("Next page")).toBeDisabled();
    expect(screen.getByTestId("pager")).toHaveTextContent("Newest first · 25 per page");
  });

  it("offers a retry when the API fails", async () => {
    const fail = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<TxsPage />, { api: api(fail) });
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    await vi.waitFor(() => expect(fail).toHaveBeenCalledTimes(2));
  });
});
