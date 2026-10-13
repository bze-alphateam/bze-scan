import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ExplorerApi } from "@/api/client";
import type { Page, Proposal, ProposalSummary, Stats } from "@/api/types";
import { Footer } from "@/components/layout/footer";
import { fakeApi, renderWithProviders } from "@/test/render";

import blocks from "../../../test/fixtures/api/v1/blocks.json";
import proposals from "../../../test/fixtures/api/v1/proposals.json";
import upgrade from "../../../test/fixtures/api/v1/proposals/48.json";
import stats from "../../../test/fixtures/api/v1/stats.json";

import { BlocksPage } from "./blocks-page";
import { FutureBlockPage } from "./future-block";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

// The stats' latest block, 25,120,450, was produced right "now"; block time 5.8 s (100k window).
const NOW = Date.parse(stats.latest_time);

/** Proposal 48 votes on v8.2.0 at 25,200,000; 47 passed an upgrade the chain is already past. */
function proposal(id: number): Promise<Proposal> {
  const p = upgrade as unknown as Proposal;
  if (id === 48) return Promise.resolve(p);
  const past = JSON.parse(JSON.stringify(p)) as Proposal & { messages: { plan: { name: string; height: string } }[] };
  past.messages[0].plan = { ...past.messages[0].plan, name: "v8.1.0", height: "25000000" };
  return Promise.resolve({ ...past, id, status: "passed" });
}

function api(extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    stats: () => Promise.resolve(stats as unknown as Stats),
    proposals: () => Promise.resolve(proposals as Page<ProposalSummary>),
    proposal: (id) => proposal(id),
    ...extra,
  });
}

async function render(props: { height?: string; date?: string } = {}, extra: Partial<ExplorerApi> = {}) {
  renderWithProviders(<FutureBlockPage {...props} />, { api: api(extra) });
  await act(() => vi.advanceTimersByTimeAsync(10));
}

const heightBox = () => screen.getByRole("textbox", { name: "Block height" });
const type = (box: HTMLElement, value: string) => fireEvent.change(box, { target: { value } });

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
});

afterEach(() => {
  vi.useRealTimers();
});

describe("FutureBlockPage", () => {
  it("counts down to a typed height with commas, ticking every second", async () => {
    await render({ height: "25,135,000" });
    expect(screen.getByRole("heading", { level: 1, name: "Future block" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Blocks/Future block");
    expect(screen.getByText(/^Latest block/)).toHaveTextContent("Latest block 25,120,450");

    // 14,550 blocks × 5.8 s = 23 h 26 min 30 s.
    const c = screen.getByTestId("future-countdown");
    expect(c).toHaveTextContent("Block 25,135,000 in about");
    expect(within(c).getByTestId("countdown-days")).toHaveTextContent("00");
    expect(within(c).getByTestId("countdown-hours")).toHaveTextContent("23");
    expect(within(c).getByTestId("countdown-minutes")).toHaveTextContent("26");
    expect(within(c).getByTestId("countdown-seconds")).toHaveTextContent("30");
    expect(screen.getByTestId("future-when")).toHaveTextContent("Around Oct 12, 2026, 11:26 UTC");
    expect(screen.getByTestId("future-when")).toHaveTextContent("your time");
    // The averages differ by 0.02 s: 0.34% of 23 h 26 min.
    expect(screen.getByTestId("future-left")).toHaveTextContent("14,550 blocks to go");
    expect(screen.getByTestId("future-left")).toHaveTextContent("give or take 4 min");

    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(screen.getByTestId("countdown-seconds")).toHaveTextContent("29");

    type(heightBox(), "25135001");
    expect(screen.getByTestId("future-countdown")).toHaveTextContent("Block 25,135,001 in about");
  });

  it("says a height at or below the tip is produced, and asks for a height on bad input", async () => {
    await render({ height: "25,120,000" });
    expect(screen.getByTestId("future-message")).toHaveTextContent("Block 25,120,000 is already produced.");
    expect(screen.getByRole("link", { name: "Open block 25,120,000" })).toHaveAttribute("href", "/blocks/25120000");

    for (const bad of ["", "abc", "25,13,5000"]) {
      type(heightBox(), bad);
      expect(screen.getByTestId("future-message")).toHaveTextContent("Type a block height, like 26,000,000.");
    }
  });

  it("estimates the height on a date and time typed day.month.year, 24-hour", async () => {
    await render({ date: "2026-10-12T12:00" });
    expect(screen.getByRole("tab", { name: "By date" })).toHaveAttribute("aria-selected", "true");
    const date = screen.getByRole("textbox", { name: "Date, day.month.year" });
    const time = screen.getByRole("textbox", { name: "Time, 24-hour" });
    expect(date).toHaveValue("12.10.2026");
    expect(time).toHaveValue("12:00");

    type(date, "18.10.2026");
    type(time, "05:00");
    const at = new Date(2026, 9, 18, 5, 0).getTime();
    const height = Math.round(25_120_450 + (at - NOW) / 5800);
    expect(screen.getByTestId("future-height")).toHaveTextContent(height.toLocaleString("en-US"));
    expect(screen.getByTestId("future-date-result")).toHaveTextContent("Oct 18, 2026, 05:00 (");
  });

  it("rejects an impossible date or time and a date in the past", async () => {
    await render({ date: "2026-10-12T12:00" });
    const date = screen.getByRole("textbox", { name: "Date, day.month.year" });
    const time = screen.getByRole("textbox", { name: "Time, 24-hour" });
    const invalid = "Type the date as day.month.year and the time as 24-hour, like 18.10.2026 and 05:00.";

    type(date, "31.02.2026");
    expect(screen.getByTestId("future-message")).toHaveTextContent(invalid);
    type(date, "18.10.2026");
    type(time, "24:00");
    expect(screen.getByTestId("future-message")).toHaveTextContent(invalid);

    type(date, "01.10.2026");
    type(time, "05:00");
    expect(screen.getByTestId("future-message")).toHaveTextContent("That date is in the past. Past blocks are on the Blocks page.");
    expect(screen.getByRole("link", { name: "Blocks page" })).toHaveAttribute("href", "/blocks");
  });

  it("switches to the countdown from a date's height", async () => {
    await render({ date: "2026-10-12T12:00" });
    const height = screen.getByTestId("future-height").textContent ?? "";
    fireEvent.click(screen.getByRole("button", { name: `Count down to block ${height}` }));
    expect(screen.getByRole("tab", { name: "By block height" })).toHaveAttribute("aria-selected", "true");
    expect(heightBox()).toHaveValue(height);
    expect(screen.getByTestId("future-countdown")).toHaveTextContent(`Block ${height} in about`);
  });

  it("lists the upgrades still ahead of the tip and counts down to one", async () => {
    await render();
    const rows = screen.getAllByTestId("planned-row");
    // 47's v8.1.0 at 25,000,000 is behind the tip.
    expect(rows).toHaveLength(1);
    expect(within(rows[0]).getByRole("link", { name: "Upgrade to v8.2.0" })).toHaveAttribute("href", "/proposals/48");
    expect(rows[0]).toHaveTextContent("at block 25,200,000 · proposal #48, voting, ends");
    expect(rows[0]).toHaveTextContent("happens only if it passes");

    fireEvent.click(within(rows[0]).getByRole("button", { name: "Count down" }));
    expect(heightBox()).toHaveValue("25,200,000");
    expect(screen.getByTestId("future-countdown")).toHaveTextContent("Block 25,200,000 in about");
  });

  it("says when no upgrade is planned", async () => {
    await render({}, { proposals: () => Promise.resolve({ items: [], next_cursor: null }) });
    expect(screen.getByTestId("planned-empty")).toHaveTextContent("No upgrade is planned right now.");
  });

  it("explains the averages and marks the one used", async () => {
    await render();
    const rows = screen.getAllByTestId("window-row");
    expect(rows.map((r) => r.textContent)).toEqual([
      "10,000 blocks5.81 s16 hours",
      "30,000 blocks5.80 s2 days",
      "50,000 blocks5.79 s3.4 days",
      "100,000 blocksused5.80 s6.7 days",
    ]);
    expect(screen.getByTestId("future-how-note")).toHaveTextContent(
      "The estimate uses the 100,000-block average. The “give or take” is how far the averages differ (0.02 s, about 0.3%), applied to the time left.",
    );
  });

  it("has nothing to show without any block time", async () => {
    await render({ height: "25,135,000" }, {
      stats: () => Promise.resolve({ ...(stats as unknown as Stats), avg_block_time_ms: null, block_time_windows: [] }),
    });
    expect(screen.getByTestId("future-no-history")).toHaveTextContent("Not enough history to estimate yet.");
    expect(screen.queryByTestId("future-countdown")).not.toBeInTheDocument();
  });
});

describe("Future block entry points", () => {
  it("links from the Blocks page title", async () => {
    renderWithProviders(<BlocksPage top={null} page={1} />, {
      api: fakeApi({
        blocks: () => Promise.resolve(blocks as never),
        stats: () => Promise.resolve(stats as unknown as Stats),
        status: () => new Promise(() => {}),
      }),
    });
    expect(screen.getByRole("link", { name: "Future block" })).toHaveAttribute("href", "/blocks/future");
  });

  it("is in the footer between Staking and Parameters", () => {
    renderWithProviders(<Footer />, { api: fakeApi({ status: () => new Promise(() => {}) }) });
    const names = within(screen.getByRole("navigation", { name: "Footer" }))
      .getAllByRole("link")
      .map((l) => l.textContent);
    expect(names.slice(names.indexOf("Staking"), names.indexOf("Parameters") + 1)).toEqual(["Staking", "Future block", "Parameters"]);
    expect(screen.getByRole("link", { name: "Future block" })).toHaveAttribute("href", "/blocks/future");
  });
});
