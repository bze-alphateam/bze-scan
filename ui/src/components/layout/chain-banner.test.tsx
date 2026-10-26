import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Halted, Status } from "@/api/types";
import { fakeApi, renderWithProviders } from "@/test/render";

import { ChainBanner } from "./chain-banner";

const healthy: Status = {
  live_fill: { healthy: true, checked_at: null, db_height: 100, node_height: 100, archive_height: 100 },
  back_fill: { status: "finished", oldest_height: 1 },
};
const behind: Status = { ...healthy, live_fill: { ...healthy.live_fill, healthy: false, db_height: 88 } };

const halted: Halted = {
  halted: true,
  height: 25263600,
  halt_height: 25263601,
  proposal_id: 48,
  name: "v8.2.0",
  round: 0,
  halted_since: new Date(Date.now() - 5 * 60_000).toISOString(),
  checked_at: new Date().toISOString(),
  total_power: "100",
  back_power: "58",
  prevote_power: "58",
  precommit_power: "0",
  members: [],
};

describe("ChainBanner", () => {
  it("shows nothing while all is well", async () => {
    renderWithProviders(<ChainBanner />, {
      api: fakeApi({ liveHalt: async () => ({ halted: false }), status: async () => healthy }),
    });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("says the chain is paused for the upgrade and links to who is missing", async () => {
    renderWithProviders(<ChainBanner />, { api: fakeApi({ liveHalt: async () => halted, status: async () => behind }) });
    const banner = await screen.findByTestId("halt-banner");
    expect(banner).toHaveTextContent("The chain is paused for the v8.2.0 upgrade (for 5 min).");
    expect(banner).toHaveTextContent("58.0% of the voting power is back, 8.7% more is needed on the new version.");
    expect(screen.getByRole("link", { name: "See who is missing" })).toHaveAttribute("href", "/proposals/48");
    expect(screen.queryByTestId("behind-banner")).toBeNull();
  });

  it("says the explorer is behind when no upgrade halted the chain", async () => {
    renderWithProviders(<ChainBanner />, { api: fakeApi({ liveHalt: async () => ({ halted: false }), status: async () => behind }) });
    await waitFor(() => expect(screen.getByTestId("behind-banner")).toHaveTextContent("The explorer is 12 blocks behind the chain."));
    expect(screen.queryByTestId("halt-banner")).toBeNull();
  });
});
