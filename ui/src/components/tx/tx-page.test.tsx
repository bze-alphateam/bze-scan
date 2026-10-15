import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import { live, TX_PENDING_POLL_MS, TX_PENDING_WINDOW_MS } from "@/api/policy";
import type { Block, Page, RawTx, Status, TokenSummary, Tx, ValidatorList } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import block from "../../../test/fixtures/api/v1/blocks/25120440.json";
import rawSend from "../../../test/fixtures/api/v1/raw/tx/3F9A6B2C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B4C5D0C21E.json";
import status from "../../../test/fixtures/api/v1/status.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";
import failedTx from "../../../test/fixtures/api/v1/txs/7D04E1A2B3C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B40E9B0.json";
import sendTx from "../../../test/fixtures/api/v1/txs/3F9A6B2C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B4C5D0C21E.json";
import dexTx from "../../../test/fixtures/api/v1/txs/5D1E0A7B3C9F42E6A8D0B1C2D3E4F5061728394A5B6C7D8E9F0A1B2C3D4E5F60.json";
import ibcTx from "../../../test/fixtures/api/v1/txs/9C2B4E6A8D0F1325476980ABCDEF0123456789ABCDEF0123456789ABCDEF0A7E.json";
import validators from "../../../test/fixtures/api/v1/validators.json";

import { TxPage } from "./tx-page";

const SEND = sendTx.hash;
const FAILED = failedTx.hash;

function api(tx: unknown, extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    transaction: () => Promise.resolve(tx as Tx),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    validators: () => Promise.resolve(validators as ValidatorList),
    block: () => Promise.resolve(block as unknown as Block),
    status: () => Promise.resolve(status as Status),
    rawTx: () => Promise.resolve(rawSend as RawTx),
    ...extra,
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe("TxPage", () => {
  it("shows the page skeleton on the first load", () => {
    renderWithProviders(<TxPage hash={SEND} />, { api: pendingApi() });
    expect(screen.getByTestId("tx-skeleton")).toBeInTheDocument();
  });

  it("renders a send: header, one action, the collapsed flow, the overview and the details", async () => {
    const rawTx = vi.fn(() => Promise.resolve(rawSend as RawTx));
    renderWithProviders(<TxPage hash={SEND} />, { api: api(sendTx, { rawTx }) });

    expect(await screen.findByRole("heading", { level: 1, name: "Transaction" })).toBeInTheDocument();
    expect(screen.getByText("Success")).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Transactions/3F9A…C21E");
    expect(screen.getByTestId("tx-hash")).toHaveTextContent(SEND);

    const actions = screen.getAllByTestId("action-card");
    expect(actions).toHaveLength(1);
    expect(within(actions[0]).getByRole("heading", { name: "Send 10 BZE to bze1m3k…9ds4" })).toBeInTheDocument();
    expect(within(actions[0]).queryByText("not executed")).not.toBeInTheDocument();
    expect(await within(actions[0]).findByText("≈ $0.02")).toBeInTheDocument();

    expect(screen.getByTestId("flow-summary")).toHaveTextContent(
      "2 transfers · 0.002 BZE fee to Fee collector · 10 BZE to bze1m3k…9ds4",
    );
    await userEvent.click(screen.getByRole("button", { name: /Show steps/ }));
    const steps = screen.getAllByTestId("flow-step");
    expect(steps.map((s) => s.textContent)).toEqual([
      expect.stringContaining("transaction fee"),
      expect.stringContaining("sent"),
    ]);
    expect(screen.queryByTestId("net-box")).not.toBeInTheDocument();

    expect(await screen.findByText("3rd of 7 transactions in the block")).toBeInTheDocument();
    expect(await screen.findByText(/· 10 confirmations/)).toBeInTheDocument();
    const overview = screen.getByRole("heading", { name: "Overview" }).closest("section")!;
    expect(within(overview).getByText("rent for October")).toBeInTheDocument();
    expect(within(overview).getByText("0.002 BZE")).toBeInTheDocument();

    expect(rawTx).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: /More details/ }));
    // Flex gaps space the parts, so the text runs together.
    expect(screen.getByText("Gas").nextElementSibling).toHaveTextContent("81,234of200,000");
    expect(await screen.findByText(/"gas_used": "81234"/)).toBeInTheDocument();
    expect(rawTx).toHaveBeenCalledWith(SEND, expect.anything());
    // One on the raw bar from c2 up, one under the JSON on phones.
    const pings = screen.getAllByRole("link", { name: /Open on Ping.pub/ });
    expect(pings).toHaveLength(2);
    for (const a of pings) expect(a).toHaveAttribute("href", expect.stringMatching(new RegExp(`/tx/${SEND}$`)));
  });

  it("explains a failed transaction and shows its fee only", async () => {
    renderWithProviders(<TxPage hash={FAILED} />, { api: api(failedTx) });

    expect(await screen.findByText("Failed")).toBeInTheDocument();
    const why = screen.getByRole("alert");
    expect(within(why).getByText("Not enough BZE for the amount plus the fee")).toBeInTheDocument();
    expect(within(why).getByText(/insufficient funds$/)).toBeInTheDocument();
    expect(within(screen.getByTestId("action-card")).getByText("not executed")).toBeInTheDocument();
    expect(screen.getByTestId("flow-summary")).toHaveTextContent("1 transfer · 0.002 BZE fee to Fee collector");
  });

  it("looks for a pending transaction every 4 s, then says it is not found", async () => {
    vi.useFakeTimers();
    const transaction = vi.fn(() => Promise.reject(new ApiError(404, "not_found", "transaction not indexed")));
    renderWithProviders(<TxPage hash={SEND} />, { api: api(null, { transaction }) });

    await act(() => vi.advanceTimersByTimeAsync(0));
    const looking = screen.getByTestId("tx-looking");
    expect(looking).toHaveTextContent("Looking for this transaction…If you just sent it, it appears within seconds.");
    expect(looking).toHaveAttribute("data-state", "info");
    expect(looking).toHaveAttribute("role", "status");
    expect(screen.getByRole("heading", { level: 1, name: "Transaction" })).toBeInTheDocument();
    expect(screen.getByTestId("tx-hash")).toHaveTextContent(SEND);
    expect(transaction).toHaveBeenCalledTimes(1);

    await act(() => vi.advanceTimersByTimeAsync(TX_PENDING_POLL_MS));
    expect(transaction).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId("tx-looking")).toBeInTheDocument();

    await act(() => vi.advanceTimersByTimeAsync(TX_PENDING_WINDOW_MS));
    const notFound = screen.getByTestId("tx-not_found");
    expect(within(notFound).getByRole("heading", { name: "Not found" })).toBeInTheDocument();
    expect(notFound).toHaveTextContent("transactions older than the explorer's history are not indexed");
    expect(notFound).toHaveAttribute("data-state", "neutral");
    expect(within(notFound).getByRole("link", { name: /Ping.pub/ })).toHaveAttribute("href", expect.stringContaining(`/tx/${SEND}`));
    expect(screen.queryByText("Failed")).not.toBeInTheDocument();

    const calls = transaction.mock.calls.length;
    await act(() => vi.advanceTimersByTimeAsync(TX_PENDING_POLL_MS * 3));
    expect(transaction).toHaveBeenCalledTimes(calls);
  });

  it("shows the transaction once a pending one is indexed", async () => {
    vi.useFakeTimers();
    let found = false;
    const transaction = vi.fn(() =>
      found ? Promise.resolve(sendTx as unknown as Tx) : Promise.reject(new ApiError(404, "not_found", "not indexed")),
    );
    renderWithProviders(<TxPage hash={SEND} />, { api: api(null, { transaction }) });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getByTestId("tx-looking")).toBeInTheDocument();

    found = true;
    // React Query notifies on a 0 ms timer after the poll's answer.
    await act(() => vi.advanceTimersByTimeAsync(TX_PENDING_POLL_MS + 10));
    expect(screen.getByText("Success")).toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(TX_PENDING_WINDOW_MS));
    expect(transaction).toHaveBeenCalledTimes(2);
  });

  it("offers a retry when the API fails", async () => {
    const transaction = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<TxPage hash={SEND} />, { api: api(null, { transaction }) });
    expect(await screen.findByRole("heading", { name: "This transaction could not be loaded" })).toBeInTheDocument();
    expect(screen.getByTestId("tx-error")).toHaveAttribute("data-state", "error");
    expect(screen.getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Transactions/3F9A…C21E");
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(transaction).toHaveBeenCalledTimes(2);
  });
});

describe("TxPage with BZE actions", () => {
  it("renders a DEX order and a claim: the outcome, the steps with their reasons and the net box", async () => {
    renderWithProviders(<TxPage hash={dexTx.hash} />, { api: api(dexTx) });
    const actions = await screen.findAllByTestId("action-card");
    expect(actions.map((a) => a.getAttribute("data-category"))).toEqual(["dex", "rewards"]);
    expect(within(actions[0]).getByRole("heading", { name: "Buy order · 1,000 BZE at 0.0125 USDC" })).toBeInTheDocument();
    expect(within(actions[0]).getByTestId("dex-fills")).toHaveTextContent("Filled right away: 600 BZE for 7.5 USDC");
    expect(within(actions[0]).getByTestId("dex-placed")).toHaveTextContent("400 BZE still waiting on the order book");
    expect(within(actions[1]).getByRole("heading", { name: "Claim staking-reward prizes · 1.5 BZE" })).toBeInTheDocument();

    // A DEX transaction opens its steps.
    const steps = screen.getAllByTestId("flow-step");
    expect(steps).toHaveLength(5);
    expect(steps.map((s) => s.textContent)).toEqual([
      expect.stringContaining("transaction fee"),
      expect.stringContaining("DEX fee"),
      expect.stringContaining("DEX fee"),
      expect.stringContaining("held for the buy order"),
      expect.stringContaining("staking-reward prize"),
    ]);
    const net = screen.getByTestId("net-box");
    expect(net).toHaveTextContent(/^Net for /);
    expect(net).toHaveTextContent(/fee$/);
  });

  it("polls a pending DEX transaction until the order book's outcome arrives", async () => {
    vi.useFakeTimers();
    const pending = { ...dexTx, dex: { placed: [], fills: [], cancelled: [], pending: true } };
    let resolved = false;
    const transaction = vi.fn(() => Promise.resolve((resolved ? dexTx : pending) as unknown as Tx));
    renderWithProviders(<TxPage hash={dexTx.hash} />, { api: api(null, { transaction }) });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getByTestId("dex-pending")).toBeInTheDocument();

    resolved = true;
    await act(() => vi.advanceTimersByTimeAsync(live.refetchInterval + 10));
    expect(screen.queryByTestId("dex-pending")).not.toBeInTheDocument();
    expect(screen.getByTestId("dex-fills")).toBeInTheDocument();

    // Settled: immutable again.
    const calls = transaction.mock.calls.length;
    await act(() => vi.advanceTimersByTimeAsync(live.refetchInterval * 3));
    expect(transaction).toHaveBeenCalledTimes(calls);
  });

  it("polls a cross-chain transfer until it is delivered, but not an expired one", async () => {
    vi.useFakeTimers();
    const row = ibcTx.ibc_transfers[0];
    const delivered = { ...ibcTx, ibc_transfers: [{ ...row, status: "delivered", result_tx_hash: "CD".repeat(32), result_time: "2026-10-11T09:16:00Z" }] };
    let done = false;
    const transaction = vi.fn(() => Promise.resolve((done ? delivered : ibcTx) as unknown as Tx));
    const { unmount } = renderWithProviders(<TxPage hash={ibcTx.hash} />, { api: api(null, { transaction }) });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getByRole("heading", { name: "Cross-chain transfer to Cosmos Hub · 2 ATOM" })).toBeInTheDocument();
    expect(screen.getByTestId("ibc-status")).toHaveTextContent("Pending");

    done = true;
    await act(() => vi.advanceTimersByTimeAsync(live.refetchInterval + 10));
    expect(screen.getByTestId("ibc-status")).toHaveTextContent("Delivered");
    unmount();

    const expired = { ...ibcTx, ibc_transfers: [{ ...row, expired: true }] };
    const once = vi.fn(() => Promise.resolve(expired as unknown as Tx));
    renderWithProviders(<TxPage hash={ibcTx.hash} />, { api: api(null, { transaction: once }) });
    await act(() => vi.advanceTimersByTimeAsync(live.refetchInterval * 3));
    expect(screen.getByTestId("ibc-status")).toHaveTextContent("Expired, waiting for the refund");
    expect(once).toHaveBeenCalledTimes(1);
  });
});
