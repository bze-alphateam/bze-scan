import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { IbcChannel, Page, RateLimitList } from "@/api/types";
import { fakeApi, renderWithProviders } from "@/test/render";

import channelsJson from "../../../test/fixtures/api/v1/ibc/channels.json";
import limitsJson from "../../../test/fixtures/api/v1/ibc/rate-limits.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";

import { IbcPage } from "./ibc-page";

const channels = channelsJson as Page<IbcChannel>;
const limits = limitsJson as RateLimitList;

function api(extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    ibcChannels: () => Promise.resolve(channels),
    rateLimits: () => Promise.resolve(limits),
    tokens: () => Promise.resolve(tokens as never),
    ...extra,
  });
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"], now: new Date("2099-10-11T18:48:00Z") });
});
afterEach(() => vi.useRealTimers());

describe("IbcPage", () => {
  it("lists every channel with its chain, state and client status", async () => {
    renderWithProviders(<IbcPage />, { api: api() });
    const cards = await screen.findAllByTestId("channel-card");
    expect(cards).toHaveLength(8);
    expect(within(cards[0]).getByTestId("channel-chain")).toHaveTextContent("Osmosis");
    expect(within(cards[0]).getByTestId("channel-id")).toHaveTextContent("channel-0");
    expect(within(cards[0]).getByTestId("channel-state")).toHaveTextContent("Open");
    expect(within(cards[0]).getByTestId("channel-client")).toHaveTextContent("Client active");
    expect(within(cards[6]).getByTestId("channel-client")).toHaveTextContent("Client expired");
  });

  it("shows each channel's limits with the used bars and the reset", async () => {
    renderWithProviders(<IbcPage />, { api: api() });
    const [osmosis] = await screen.findAllByTestId("channel-card");
    expect(within(osmosis).getByTestId("limit-out")).toHaveTextContent("1.2M of 2M BZE used (60%)");
    expect(within(osmosis).getByTestId("limit-in")).toHaveTextContent("net inflow 300K BZE, room above the limit");
    expect(within(osmosis).getByTestId("limit-reset")).toHaveTextContent("resets in 5 h 12 m (00:00 UTC)");
    // Noble carries the 100% / 0% route of USDC.n next to BZE's.
    const noble = screen.getAllByTestId("channel-card")[2];
    const rows = within(noble).getAllByTestId("limit-row");
    expect(rows).toHaveLength(2);
    expect(within(rows[1]).getByTestId("limit-out")).toHaveTextContent("No practical limit");
    expect(within(rows[1]).getByTestId("limit-in")).toHaveTextContent("Incoming transfers of this token are not allowed on this route");
  });

  it("turns the bar red at 90% used", async () => {
    renderWithProviders(<IbcPage />, { api: api() });
    const atone = (await screen.findAllByTestId("channel-card"))[3];
    expect(within(atone).getAllByRole("progressbar", { name: "Out used" })[0]).toHaveAttribute("data-tone", "bad");
  });

  it("explains the limits in the ? pop-up", async () => {
    renderWithProviders(<IbcPage />, { api: api() });
    await userEvent.setup({ advanceTimers: vi.advanceTimersByTime }).click(await screen.findByTestId("limits-help"));
    expect(await screen.findByText(/Limits count the net flow \(out minus in\) of a 24-hour window/)).toBeInTheDocument();
  });

  it("lists the tokens blocked from crossing chains", async () => {
    renderWithProviders(<IbcPage />, { api: api() });
    const blocked = await screen.findByTestId("blocked-denoms");
    expect(within(blocked).getByRole("link")).toHaveAttribute("href", expect.stringContaining("uhoney"));
  });

  it("hides the blocked list when it is empty", async () => {
    renderWithProviders(<IbcPage />, { api: api({ rateLimits: () => Promise.resolve({ ...limits, blacklisted_denoms: [] }) }) });
    await screen.findAllByTestId("channel-card");
    expect(screen.queryByTestId("blocked-denoms")).not.toBeInTheDocument();
  });

  it("still lists the channels, without limits, before chain v8.3.0", async () => {
    renderWithProviders(<IbcPage />, { api: api({ rateLimits: () => Promise.resolve({ supported: false, items: [], blacklisted_denoms: [] }) }) });
    expect(await screen.findAllByTestId("channel-card")).toHaveLength(8);
    expect(screen.getByTestId("limits-unsupported")).toHaveTextContent("IBC limits arrive with chain v8.3.0");
    expect(screen.queryByTestId("limit-row")).not.toBeInTheDocument();
    expect(screen.queryByTestId("limits-help")).not.toBeInTheDocument();
  });

  it("offers a retry when the channels cannot be loaded", async () => {
    renderWithProviders(<IbcPage />, { api: api({ ibcChannels: () => Promise.reject(new ApiError(502, "upstream_error", "down")) }) });
    expect(await screen.findByTestId("ibc-error")).toHaveTextContent("The channels could not be loaded");
  });

  it("says so, with a retry, when the limits cannot be loaded, and still lists the channels", async () => {
    const rateLimits = vi.fn().mockRejectedValueOnce(new ApiError(502, "upstream_error", "down")).mockResolvedValue(limits);
    renderWithProviders(<IbcPage />, { api: api({ rateLimits }) });
    expect(await screen.findAllByTestId("channel-card")).toHaveLength(8);
    const note = await screen.findByTestId("limits-error");
    expect(note).toHaveTextContent("The limits could not be loaded");
    await userEvent.setup({ advanceTimers: vi.advanceTimersByTime }).click(within(note).getByRole("button", { name: "Try again" }));
    expect(await screen.findAllByTestId("limit-row")).not.toHaveLength(0);
    expect(screen.queryByTestId("limits-error")).not.toBeInTheDocument();
  });

  it("shows the limits of a channel the list does not have under Other channels", async () => {
    const stray = { ...limits.items[0], channel_id: "channel-99" };
    renderWithProviders(<IbcPage />, { api: api({ rateLimits: () => Promise.resolve({ ...limits, items: [...limits.items, stray] }) }) });
    const other = await screen.findByTestId("other-channels");
    expect(within(other).getByTestId("limit-row")).toHaveTextContent("BZE · channel-99");
  });

  it("calls a channel without a state Unknown", async () => {
    const stateless = { ...channels, items: [{ ...channels.items[0], state: null }] };
    renderWithProviders(<IbcPage />, { api: api({ ibcChannels: () => Promise.resolve(stateless) }) });
    expect(await screen.findByTestId("channel-state")).toHaveTextContent("Unknown");
  });

  it("lists an unknown blocked token by its raw denom", async () => {
    const long = "factory/bze1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq/uunknown";
    renderWithProviders(<IbcPage />, { api: api({ rateLimits: () => Promise.resolve({ ...limits, blacklisted_denoms: [long] }) }) });
    const link = await within(await screen.findByTestId("blocked-denoms")).findByRole("link", { name: long });
    expect(link).toHaveAttribute("href", expect.stringContaining("uunknown"));
  });

  it("keeps the reset countdown moving while the polled data does not change", async () => {
    vi.useRealTimers();
    vi.useFakeTimers({ now: new Date("2099-10-11T18:48:00Z"), shouldAdvanceTime: false });
    renderWithProviders(<IbcPage />, { api: api() });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    const [osmosis] = screen.getAllByTestId("channel-card");
    expect(within(osmosis).getByTestId("limit-reset")).toHaveTextContent("resets in 5 h 12 m (00:00 UTC)");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(within(osmosis).getByTestId("limit-reset")).toHaveTextContent("resets in 5 h 11 m (00:00 UTC)");
  });
});
