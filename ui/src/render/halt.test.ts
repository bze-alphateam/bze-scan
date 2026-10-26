import { describe, expect, it } from "vitest";

import type { Halted, Status } from "@/api/types";

import { backLine, behindLine, haltedFor, haltProgress, missingMembers } from "./halt";

const member = (cons: string, power: string, back = false) => ({
  consensus_address: cons,
  operator_address: null,
  moniker: null,
  voting_power: power,
  back,
  prevote: back,
  precommit: false,
});

const halted: Halted = {
  halted: true,
  height: 25263600,
  halt_height: 25263601,
  proposal_id: 48,
  name: "v8.2.0",
  round: 3,
  halted_since: "2026-10-26T17:00:00Z",
  checked_at: "2026-10-26T17:12:00Z",
  total_power: "1000",
  back_power: "580",
  prevote_power: "580",
  precommit_power: "0",
  members: [member("AAAA", "500", true), member("BBBB", "300"), member("CCCC", "120"), member("DDDD", "80", true)],
};

const status = (live: Partial<Status["live_fill"]>): Status => ({
  live_fill: { healthy: false, checked_at: null, db_height: 100, node_height: 100, archive_height: 100, ...live },
  back_fill: { status: "finished", oldest_height: 1 },
});

describe("halt", () => {
  it("measures the voting power back against two thirds", () => {
    const p = haltProgress(halted);
    expect(p.back.toFixed(1)).toBe("58.0");
    expect(p.prevote.toFixed(1)).toBe("58.0");
    expect(p.precommit.toFixed(1)).toBe("0.0");
    expect(p.short.toFixed(1)).toBe("8.7");
    expect(backLine(p)).toBe("58.0% of the voting power is back, 8.7% more is needed");
  });

  it("says when enough is back", () => {
    const p = haltProgress({ ...halted, back_power: "700" });
    expect(p.short.isZero()).toBe(true);
    expect(backLine(p)).toBe("70.0% of the voting power is back, enough to restart");
  });

  it("lists the missing validators by power", () => {
    expect(missingMembers(halted).map((m) => m.consensus_address)).toEqual(["BBBB", "CCCC"]);
  });

  it("tells how long the chain has been halted", () => {
    expect(haltedFor(halted, new Date("2026-10-26T17:00:30Z"))).toBe("under a minute");
    expect(haltedFor(halted, new Date("2026-10-26T17:12:00Z"))).toBe("12 min");
  });

  it("says the explorer is behind, never that the chain stopped", () => {
    expect(behindLine(undefined)).toBeNull();
    expect(behindLine(status({ healthy: true }))).toBeNull();
    expect(behindLine(status({ db_height: 90, node_height: 100 }))).toMatch(/^The explorer is 10 blocks behind the chain\. The chain itself is running/);
    expect(behindLine(status({ db_height: 99, node_height: 100 }))).toMatch(/^The explorer is 1 block behind/);
    expect(behindLine(status({ node_height: null }))).toMatch(/^The explorer is not keeping up with the chain right now\. The chain itself is not affected/);
  });
});
