import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Halted } from "@/api/types";
import { fakeApi, renderWithProviders } from "@/test/render";

import { LiveHaltPanel } from "./live-halt";

const halted: Halted = {
  halted: true,
  height: 25263600,
  halt_height: 25263601,
  proposal_id: 48,
  name: "v8.2.0",
  round: 2,
  halted_since: new Date(Date.now() - 12 * 60_000).toISOString(),
  checked_at: new Date().toISOString(),
  total_power: "1000",
  back_power: "580",
  prevote_power: "580",
  precommit_power: "0",
  members: [
    { consensus_address: "AAAA", operator_address: "bzevaloper1a", moniker: "Alpha", voting_power: "580", back: true, prevote: true, precommit: false },
    { consensus_address: "BBBB", operator_address: "bzevaloper1b", moniker: "Bravo", voting_power: "300", back: false, prevote: false, precommit: false },
    { consensus_address: "CCCC0000000000000000", operator_address: null, moniker: null, voting_power: "120", back: false, prevote: false, precommit: false },
  ],
};

const api = (h: Halted | { halted: false }) => fakeApi({ liveHalt: async () => h, validators: () => new Promise(() => {}) });

describe("LiveHaltPanel", () => {
  it("shows the voting power back and the validators still missing", async () => {
    renderWithProviders(<LiveHaltPanel />, { api: api(halted) });
    const panel = await screen.findByTestId("live-halt");
    expect(panel).toHaveTextContent("Waiting at block 25,263,601 for 12 min, round 2.");
    expect(screen.getByTestId("live-halt-line")).toHaveTextContent("58.0% of the voting power is back, 8.7% more is needed.");
    const rows = within(screen.getByRole("list", { name: "Validators not back yet" })).getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Bravo30.0%");
    expect(rows[1]).toHaveTextContent("12.0%");
    expect(within(rows[1]).getByTitle("CCCC0000000000000000")).toBeInTheDocument();
  });

  it("stays hidden for another upgrade's proposal", async () => {
    renderWithProviders(<LiveHaltPanel proposalId={47} />, { api: api(halted) });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByTestId("live-halt")).toBeNull();
  });

  it("stays hidden when the chain is not halted", async () => {
    renderWithProviders(<LiveHaltPanel />, { api: api({ halted: false }) });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByTestId("live-halt")).toBeNull();
  });
});
