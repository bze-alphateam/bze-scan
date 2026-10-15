import { act, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import { TX_PENDING_POLL_MS, TX_PENDING_WINDOW_MS } from "@/api/policy";
import type { Block, BlockTransfer, Page, RawRpcResponse, Stats, Status, TokenSummary, ValidatorList } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import block from "../../../test/fixtures/api/v1/blocks/25120440.json";
import transfers from "../../../test/fixtures/api/v1/blocks/25120440/transfers.json";
import rawBlock from "../../../test/fixtures/api/v1/raw/block/25120440.json";
import stats from "../../../test/fixtures/api/v1/stats.json";
import status from "../../../test/fixtures/api/v1/status.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";
import validators from "../../../test/fixtures/api/v1/validators.json";

import { BlockPage } from "./block-page";

const HEIGHT = block.height;
const notIndexed = () => Promise.reject(new ApiError(404, "not_found", "block not indexed"));

function api(extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    block: () => Promise.resolve(block as unknown as Block),
    blockTransfers: () => Promise.resolve(transfers as Page<BlockTransfer>),
    status: () => Promise.resolve(status as Status),
    stats: () => Promise.resolve(stats as unknown as Stats),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    validators: () => Promise.resolve(validators as ValidatorList),
    rawBlock: () => Promise.resolve(rawBlock as RawRpcResponse),
    rawBlockResults: () => Promise.resolve({ result: { height: String(HEIGHT) } }),
    rawCommit: () => Promise.resolve({ result: { signed_header: {} } }),
    ...extra,
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe("BlockPage", () => {
  it("shows the header, overview, five transaction rows and the group lines on the first load", () => {
    renderWithProviders(<BlockPage height={HEIGHT} />, { api: pendingApi() });
    expect(screen.getByTestId("block-skeleton")).toBeInTheDocument();
    expect(screen.getByTestId("card-skeleton")).toBeInTheDocument();
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(5);
    expect(screen.getByTestId("flow-skeleton")).toBeInTheDocument();
  });

  it("renders the header, the overview and the transactions in one line each", async () => {
    renderWithProviders(<BlockPage height={HEIGHT} />, { api: api() });

    expect(await screen.findByRole("heading", { level: 1, name: "Block 25,120,440" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Blocks/25,120,440");
    expect(screen.getByRole("link", { name: "Previous block" })).toHaveAttribute("href", "/blocks/25120439");
    expect(screen.getByRole("link", { name: "Next block" })).toHaveAttribute("href", "/blocks/25120441");
    expect(screen.getByTestId("block-when")).not.toHaveTextContent("latest block");
    expect(screen.getByTestId("block-chips")).toHaveTextContent("7 transactions6 succeeded1 failed");

    const overview = screen.getByTestId("block-overview");
    expect(within(overview).getByRole("link", { name: "Honeycomb" })).toBeInTheDocument();
    expect(overview).toHaveTextContent("Fees0.022 BZE· collected from 3 transactions");
    expect(overview).toHaveTextContent("Block time5.8 s· since block25,120,439");
    expect(within(overview).getByTestId("block-hash")).toHaveTextContent(block.hash);

    const txs = screen.getAllByTestId("block-tx");
    expect(txs).toHaveLength(3);
    expect(within(txs[0]).getByTestId("block-tx-what")).toHaveTextContent("Send 10 BZE to bze1m3k…9ds4");
    expect(within(txs[1]).getByTestId("block-tx-what")).toHaveTextContent(/^Stake 500 BZE with .+ \+ 1 more action$/);
    expect(within(txs[0]).getByRole("link", { name: "3F9A…C21E" })).toHaveAttribute("href", `/txs/${block.transactions[0].hash}`);
    expect(within(txs[0]).getByTestId("block-tx-what")).toHaveAttribute("href", `/txs/${block.transactions[0].hash}`);
    expect(within(txs[2]).getByText("Failed")).toBeInTheDocument();
    expect(txs[0]).toHaveTextContent("0.002 BZE");
  });

  it("says latest block when the height is the explorer's tip", async () => {
    const tip = { ...status, live_fill: { ...status.live_fill, db_height: HEIGHT } } as Status;
    renderWithProviders(<BlockPage height={HEIGHT} />, { api: api({ status: () => Promise.resolve(tip) }) });
    expect(await screen.findByText(/· latest block/)).toBeInTheDocument();
  });

  it("says when a block has no transactions and moved no funds", async () => {
    const empty = { ...block, tx_count: 0, tx_failed_count: 0, transactions: [], transfers: [], transfers_count: 0 };
    const blockTransfers = vi.fn();
    renderWithProviders(<BlockPage height={HEIGHT} />, {
      api: api({ block: () => Promise.resolve(empty as unknown as Block), blockTransfers }),
    });
    expect(await screen.findByText("No transactions in this block")).toBeInTheDocument();
    expect(screen.getByTestId("block-chips")).toHaveTextContent(/^0 transactions$/);
    expect(screen.getByTestId("block-overview")).toHaveTextContent("Feesnone");
    expect(screen.queryByTestId("block-flow")).not.toBeInTheDocument();
    expect(blockTransfers).not.toHaveBeenCalled();
  });

  it("groups the funds that moved and expands a group into its steps", async () => {
    const user = userEvent.setup();
    renderWithProviders(<BlockPage height={HEIGHT} />, { api: api() });
    const groups = await screen.findAllByTestId("flow-group");
    expect(groups.map((g) => g.dataset.group)).toEqual([
      "fees",
      "dex",
      "staking",
      "rewards",
      "burner",
      "cross_chain",
      "governance",
      "payments",
      "other",
    ]);
    const fees = groups[0];
    expect(within(fees).getByTestId("flow-group-summary")).toHaveTextContent(
      "Fees · 0.021 BZE from 3 transactions collected by Fee collector, 0.001 BZE from 1 transaction collected by Burner fee collector",
    );

    await user.click(within(fees).getByRole("button", { name: /Show/ }));
    const steps = within(fees).getAllByTestId("flow-step");
    expect(steps).toHaveLength(4);
    expect(steps[0]).toHaveTextContent("collected by Fee collector · in");
    expect(within(steps[0]).getByRole("link", { name: "3F9A…C21E" })).toHaveAttribute("href", `/txs/${block.transactions[0].hash}`);

    const staking = groups[2];
    await user.click(within(staking).getByRole("button", { name: /Show/ }));
    expect(within(staking).getAllByTestId("by-the-chain")[0]).toHaveTextContent("block 25,120,440, by the chain");
    expect(within(staking).getAllByTestId("flow-step")[0]).toHaveTextContent("newly created ·");

    await user.click(screen.getByRole("button", { name: "Show all 18 transfers, one by one" }));
    expect(within(screen.getByTestId("block-flow-all")).getAllByTestId("flow-step")).toHaveLength(18);
    expect(screen.getByRole("button", { name: "Hide the transfers" })).toHaveAttribute("aria-expanded", "true");
  });

  it("loads gas, signatures and each raw JSON tab only when opened", async () => {
    const user = userEvent.setup();
    const raw = vi.fn(() => Promise.resolve(rawBlock as RawRpcResponse));
    const results = vi.fn(() => Promise.resolve({ result: { app_hash: "RESULTS" } }));
    renderWithProviders(<BlockPage height={HEIGHT} />, { api: api({ rawBlock: raw, rawBlockResults: results }) });

    await user.click(await screen.findByRole("button", { name: /More details/ }));
    expect(await screen.findByText("474,968")).toBeInTheDocument();
    expect(screen.getByText("used in this block")).toBeInTheDocument();
    expect(await screen.findByText("3 of 3 validators · 93.4% of voting power")).toBeInTheDocument();
    expect(await screen.findByText(/"chain_id": "beezee-1"/)).toBeInTheDocument();
    expect(raw).toHaveBeenCalledTimes(1);
    expect(results).not.toHaveBeenCalled();

    await user.click(screen.getByRole("tab", { name: "Block results" }));
    expect(await screen.findByText(/"app_hash": "RESULTS"/)).toBeInTheDocument();
    expect(results).toHaveBeenCalledTimes(1);
    // One on the tab bar (from c2), one under the JSON (phones).
    const ping = screen.getAllByRole("link", { name: /Open on Ping.pub/ });
    expect(ping).toHaveLength(2);
    for (const l of ping) expect(l).toHaveAttribute("href", expect.stringMatching(/\/block\/25120440$/));
  });

  it("waits for a height not produced yet, polling every 4 s for the pending window", async () => {
    vi.useFakeTimers();
    const ahead = status.live_fill.db_height + 1;
    const fetchBlock = vi.fn(notIndexed);
    renderWithProviders(<BlockPage height={ahead} />, { api: api({ block: fetchBlock }) });

    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getByTestId("block-not_produced")).toHaveTextContent("Not produced yet");
    expect(screen.getByRole("heading", { level: 1, name: `Block ${ahead.toLocaleString("en-US")}` })).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("as soon as it is produced");
    // The Future block estimator in place, By block height with this height, once /stats answers.
    await act(() => vi.advanceTimersByTimeAsync(10));
    expect(screen.getByRole("tab", { name: "By block height" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("textbox", { name: "Block height" })).toHaveValue(ahead.toLocaleString("en-US"));
    const first = fetchBlock.mock.calls.length;

    await act(() => vi.advanceTimersByTimeAsync(TX_PENDING_POLL_MS));
    expect(fetchBlock).toHaveBeenCalledTimes(first + 1);

    await act(() => vi.advanceTimersByTimeAsync(TX_PENDING_WINDOW_MS));
    expect(screen.getByTestId("block-not_produced")).toHaveTextContent("Reload the page to look again");
    const calls = fetchBlock.mock.calls.length;
    await act(() => vi.advanceTimersByTimeAsync(TX_PENDING_POLL_MS * 3));
    expect(fetchBlock).toHaveBeenCalledTimes(calls);
  });

  it("shows the block once a waited-for height is produced", async () => {
    vi.useFakeTimers();
    let produced = false;
    const fetchBlock = vi.fn(() => (produced ? Promise.resolve(block as unknown as Block) : notIndexed()));
    renderWithProviders(<BlockPage height={status.live_fill.db_height + 1} />, { api: api({ block: fetchBlock }) });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getByTestId("block-not_produced")).toBeInTheDocument();
    produced = true;
    await act(() => vi.advanceTimersByTimeAsync(TX_PENDING_POLL_MS + 10));
    expect(screen.getByRole("heading", { level: 1, name: "Block 25,120,440" })).toBeInTheDocument();
  });

  it("explains a height older than the explorer's history", async () => {
    const fetchBlock = vi.fn(notIndexed);
    renderWithProviders(<BlockPage height={24998000} />, { api: api({ block: fetchBlock }) });
    const card = await screen.findByTestId("block-too_old");
    expect(within(card).getByRole("heading")).toHaveTextContent("Older than the explorer's history (from block 24,998,316)");
    expect(card).toHaveTextContent("The block is on the chain, the explorer just doesn't keep blocks this old.");
    expect(card).toHaveAttribute("data-state", "neutral");
    expect(screen.getByRole("heading", { level: 1, name: "Block 24,998,000" })).toBeInTheDocument();
    expect(within(card).getByRole("link", { name: /Ping.pub/ })).toHaveAttribute("href", expect.stringMatching(/\/block\/24998000$/));
    expect(fetchBlock).toHaveBeenCalledTimes(1);
  });

  it("explains a height missing from the explorer", async () => {
    renderWithProviders(<BlockPage height={25000000} />, { api: api({ block: notIndexed }) });
    const card = await screen.findByTestId("block-missing");
    expect(within(card).getByRole("heading")).toHaveTextContent("Not indexed: this height is missing from the explorer");
    expect(card).toHaveAttribute("data-state", "warn");
    expect(within(card).getByRole("link", { name: /Ping.pub/ })).toBeInTheDocument();
  });

  it("offers a retry when the API fails", async () => {
    const fail = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<BlockPage height={HEIGHT} />, { api: api({ block: fail }) });
    const card = await screen.findByTestId("block-error");
    expect(card).toHaveAttribute("data-state", "error");
    expect(within(card).getByRole("heading")).toHaveTextContent("This block could not be loaded");
    expect(screen.getByRole("heading", { level: 1, name: "Block 25,120,440" })).toBeInTheDocument();
    fireEvent.click(within(card).getByRole("button", { name: "Try again" }));
    await vi.waitFor(() => expect(fail).toHaveBeenCalledTimes(2));
  });
});
