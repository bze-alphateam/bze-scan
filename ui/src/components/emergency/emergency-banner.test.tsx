import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Emergency, Halted } from "@/api/types";
import { ChainBanner } from "@/components/layout/chain-banner";
import { fakeApi, renderWithProviders } from "@/test/render";

import active from "../../../test/fixtures/variants/emergency-active.json";
import clear from "../../../test/fixtures/api/v1/emergency.json";
import { EmergencyBanner } from "./emergency-banner";

const api = (emergency: () => Promise<Emergency>, liveHalt: () => Promise<Halted | { halted: false }> = async () => ({ halted: false })) =>
  fakeApi({ emergency, liveHalt, status: async () => ({ live_fill: { healthy: true } }) as never });

/** Renders and waits until the emergency read has answered (or failed), so an absent banner is the answer, not a pending query. */
async function renderSettled(emergency: () => Promise<Emergency>, liveHalt?: () => Promise<Halted | { halted: false }>) {
  const read = vi.fn(emergency);
  const result = renderWithProviders(<EmergencyBanner />, { api: api(read, liveHalt) });
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(result.queryClient.getQueryState(["emergency"])?.fetchStatus).toBe("idle"));
  await waitFor(() => expect(result.queryClient.getQueryState(["upgrade", "live"])?.fetchStatus ?? "idle").toBe("idle"));
  return result;
}

describe("EmergencyBanner", () => {
  it("shows nothing while no measure is in force", async () => {
    await renderSettled(async () => clear as Emergency);
    expect(screen.queryByTestId("emergency-banner")).toBeNull();
  });

  it("shows nothing when the state cannot be read", async () => {
    const { queryClient } = await renderSettled(() => Promise.reject(new Error("502")));
    expect(queryClient.getQueryState(["emergency"])?.status).toBe("error");
    expect(screen.queryByTestId("emergency-banner")).toBeNull();
  });

  it("shows nothing on a node without the switches", async () => {
    await renderSettled(async () => ({ ...(clear as Emergency), supported: false, active: true, halted_denoms: active.halted_denoms }));
    expect(screen.queryByTestId("emergency-banner")).toBeNull();
  });

  it("names the first three parts, counts the rest and leads to the page", async () => {
    renderWithProviders(<EmergencyBanner />, { api: api(async () => active as Emergency) });
    const banner = await screen.findByTestId("emergency-banner");
    expect(banner).toHaveTextContent(
      "Emergency measures active: trading halted for 2 tokens · 1 token blocked from crossing chains · 1 channel closed + 3 more details →",
    );
    expect(screen.getByRole("link", { name: "details →" })).toHaveAttribute("href", "/emergency");
  });

  it("lists all parts when there are three or fewer", async () => {
    renderWithProviders(<EmergencyBanner />, {
      api: api(async () => ({ ...(clear as Emergency), active: true, halted_denoms: active.halted_denoms, closed_channels: active.closed_channels })),
    });
    const banner = await screen.findByTestId("emergency-banner");
    expect(banner).toHaveTextContent("trading halted for 2 tokens · 1 channel closed details →");
    expect(banner).not.toHaveTextContent("more");
  });

  it("leaves a chain halt that has stopped the chain to the pause strip", async () => {
    const paused = { halted: true, kind: "emergency", height: 25120899, halt_height: 25120900 } as unknown as Halted;
    const only = { ...(clear as Emergency), active: true, plan: active.plan };
    await renderSettled(async () => only, async () => paused);
    expect(screen.queryByTestId("emergency-banner")).toBeNull();
  });

  it("sits under the pause strip: two rows, the strip first", async () => {
    const paused = {
      halted: true,
      height: 25120899,
      halt_height: 25120900,
      proposal_id: 48,
      name: "v8.3.0",
      kind: "governance",
      reason: null,
      scheduled_by: null,
      round: 0,
      halted_since: new Date(Date.now() - 60_000).toISOString(),
      checked_at: new Date().toISOString(),
      total_power: "100",
      back_power: "58",
      prevote_power: "58",
      precommit_power: "0",
      members: [],
    } as Halted;
    renderWithProviders(
      <>
        <ChainBanner />
        <EmergencyBanner />
      </>,
      { api: api(async () => active as Emergency, async () => paused) },
    );
    const strip = await screen.findByTestId("halt-banner");
    const bar = await screen.findByTestId("emergency-banner");
    expect(strip.compareDocumentPosition(bar) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(bar).toHaveTextContent("Emergency measures active");
  });
});
