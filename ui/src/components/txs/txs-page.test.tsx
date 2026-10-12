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

  it("polls the first page, stops once an older page is loaded, and goes back to the latest", async () => {
    vi.useFakeTimers();
    const fetchTxs = vi.fn(answer);
    renderWithProviders(<TxsPage />, { api: api(fetchTxs) });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getAllByTestId("tx-row")).toHaveLength(10);
    expect(fetchTxs).toHaveBeenCalledTimes(1);

    await act(() => vi.advanceTimersByTimeAsync(7_000));
    expect(fetchTxs).toHaveBeenCalledTimes(2);
    expect(screen.queryByTestId("table-skeleton")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show older transactions" }));
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(fetchTxs).toHaveBeenLastCalledWith({ cursor: "25120440:0", limit: 25 }, expect.anything());
    expect(screen.getAllByTestId("tx-row")).toHaveLength(11);
    expect(screen.getByTestId("txs-snapshot")).toHaveTextContent("the list stopped updating");
    const calls = fetchTxs.mock.calls.length;
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(fetchTxs).toHaveBeenCalledTimes(calls);

    vi.spyOn(window, "scrollTo").mockImplementation(() => {});
    fireEvent.click(screen.getByRole("button", { name: "Back to the latest transactions" }));
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getAllByTestId("tx-row")).toHaveLength(10);
    expect(screen.queryByTestId("txs-snapshot")).not.toBeInTheDocument();
  });

  it("offers a retry when the API fails", async () => {
    const fail = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<TxsPage />, { api: api(fail) });
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    await vi.waitFor(() => expect(fail).toHaveBeenCalledTimes(2));
  });
});
