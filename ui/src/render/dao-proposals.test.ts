import { describe, expect, it } from "vitest";

import type { DaoProposal, DaoWeightDenom } from "@/api/types";

import { checkGroups, isReduced, percentOf, settleNote, shares, statusInfo, summaryText, SUMMARY_LIMIT, weightText, yesOfCast } from "./dao-proposals";

const cfg = { min_quorum: "0.400000000000000000", threshold: "0.500000000000000000", expedite_quorum: "0.600000000000000000", expedite_threshold: "0.660000000000000000" };

function proposal(extra: Partial<DaoProposal> = {}): DaoProposal {
  return {
    id: 1,
    dao_id: 2,
    title: "t",
    status: "voting",
    proposer: null,
    proposer_label: null,
    expedited: false,
    submit_time: null,
    voting_end: null,
    yes: "30",
    no: "30",
    abstain: "0",
    denominator: "100",
    settled_height: null,
    message_types: [],
    dao: null,
    summary: null,
    msgs: null,
    submit_height: null,
    submit_tx_hash: null,
    pinned_config: cfg,
    tally_updated_at: null,
    voting_ended: false,
    expedite_settleable: false,
    retry_deadline: null,
    settled_tx_hash: null,
    expedited_early: null,
    executed: null,
    execution_error: null,
    deposit_refunded: null,
    deposit_destination: null,
    deposit_amount: null,
    cancelled_height: null,
    cleaned_height: null,
    votes_count: 0,
    weight_denom: null,
    checks: { regular: { quorum_met: true, threshold_met: false, passes: false }, expedite: null },
    ...extra,
  };
}

describe("statusInfo", () => {
  it("names every status the way the ticket does", () => {
    expect(["voting", "executed", "execution_failed", "rejected", "expired", "cancelled"].map((s) => statusInfo(s).label)).toEqual([
      "Voting",
      "Passed (executed)",
      "Failed to execute",
      "Rejected",
      "Expired",
      "Cancelled",
    ]);
    expect(statusInfo(null).label).toBe("Not open");
    expect(statusInfo("something new").label).toBe("Not open");
  });
});

describe("shares", () => {
  it("measures each option and the turnout against the pinned voting power", () => {
    expect(shares({ yes: "25", no: "10", abstain: "5", denominator: "100" })).toEqual({ yes: "25", no: "10", abstain: "5", turnout: "40" });
  });

  it("is unknown without a denominator, and for a zero one", () => {
    expect(shares({ yes: "1", no: "0", abstain: "0", denominator: null }).yes).toBeNull();
    expect(shares({ yes: "1", no: "0", abstain: "0", denominator: "0" }).turnout).toBeNull();
    expect(percentOf("x", "10")).toBeNull();
  });

  it("splits a vote count too large for a float without rounding it", () => {
    expect(percentOf("123456789012345678901234567890", "246913578024691357802469135780")).toBe("50");
  });
});

describe("yesOfCast", () => {
  it("is Yes over Yes + No, abstain not counted", () => {
    expect(yesOfCast({ yes: "30", no: "10" })).toBe("75");
  });
  it("is unknown with neither a yes nor a no vote", () => {
    expect(yesOfCast({ yes: "0", no: "0" })).toBeNull();
    expect(yesOfCast({ yes: null, no: "1" })).toBeNull();
  });
});

describe("checkGroups", () => {
  it("has no checks without the API's verdicts", () => {
    expect(checkGroups(proposal({ checks: null }))).toEqual([]);
  });

  it("reports a tie as failing the threshold once voting ended, with the figures that explain it", () => {
    const [regular] = checkGroups(proposal({ status: "rejected" }));
    expect(regular.lines.map((l) => l.ok)).toEqual([true, false]);
    expect(regular.lines[0].detail).toBe("60.0% of the voting power voted · 40% needed");
    expect(regular.lines[1].detail).toBe("50.0% of the Yes and No votes are Yes · more than 50% needed, a tie fails");
  });

  it("shows a bar not met yet as not reached, not failed, while voting; a met bar stays met", () => {
    const [regular] = checkGroups(proposal({ status: "voting" }));
    expect(regular.lines.map((l) => l.ok)).toEqual([true, null]);
    const groups = checkGroups(
      proposal({ status: "voting", expedited: true, checks: { regular: { quorum_met: false, threshold_met: false, passes: false }, expedite: { quorum_met: false, yes_met: true, passes: false } } }),
    );
    expect(groups[0].lines.map((l) => l.ok)).toEqual([null, null]);
    expect(groups[1].lines.map((l) => l.ok)).toEqual([null, true]);
  });

  it("fails the unmet bars once the proposal is no longer voting", () => {
    const [regular] = checkGroups(proposal({ status: "rejected", checks: { regular: { quorum_met: false, threshold_met: false, passes: false }, expedite: null } }));
    expect(regular.lines.map((l) => l.ok)).toEqual([false, false]);
  });

  it("shows the verdicts of the API, not its own arithmetic (a boundary the API passes stays passing)", () => {
    const [regular] = checkGroups(proposal({ status: "executed", yes: "31", checks: { regular: { quorum_met: true, threshold_met: true, passes: true }, expedite: null } }));
    expect(regular.lines.map((l) => l.ok)).toEqual([true, true]);
  });

  it("adds the early-settle bars of an expedited proposal, against the full voting power", () => {
    const groups = checkGroups(
      proposal({ status: "executed", expedited: true, yes: "70", no: "0", checks: { regular: { quorum_met: true, threshold_met: true, passes: true }, expedite: { quorum_met: true, yes_met: true, passes: true } } }),
    );
    expect(groups.map((g) => g.key)).toEqual(["regular", "expedite"]);
    expect(groups[1].lines.map((l) => l.ok)).toEqual([true, true]);
    expect(groups[1].lines[1].detail).toBe("70.0% of the full voting power said Yes · more than 66% needed");
  });

  it("says the bar is unknown when the pinned rules lack it", () => {
    const [regular] = checkGroups(proposal({ pinned_config: {} }));
    expect(regular.lines[1].detail).toContain("more than the threshold needed");
  });
});

describe("settleNote", () => {
  it("says anyone can settle a proposal whose voting ended", () => {
    expect(settleNote({ status: "voting", voting_ended: true, expedite_settleable: false })).toBe("Voting ended; anyone can settle it now");
  });
  it("says an expedited one can be settled early", () => {
    expect(settleNote({ status: "voting", voting_ended: false, expedite_settleable: true })).toBe("Can be settled early");
  });
  it("says nothing otherwise, nor once settled", () => {
    expect(settleNote({ status: "voting", voting_ended: false, expedite_settleable: false })).toBeNull();
    expect(settleNote({ status: "executed", voting_ended: true, expedite_settleable: true })).toBeNull();
  });
});

describe("weightText", () => {
  const bze: DaoWeightDenom = { denom: "ubze", symbol: "BZE", exponent: 6 };
  it("writes a stake DAO's weight in display units", () => {
    expect(weightText("1500000", bze)).toBe("1.5 BZE");
  });
  it("writes the raw amount for a denom without metadata", () => {
    expect(weightText("5", { denom: "factory/x/y", symbol: null, exponent: null })).toBe("5 factory/x/y");
  });
  it("never calls the raw amount of a stake DAO with an unknown denom votes", () => {
    expect(weightText("3000000000", null, false, true)).toBe("3000000000 (base units)");
  });
  it("counts votes for a member list", () => {
    expect(weightText("1", null)).toBe("1 vote");
    expect(weightText("3", null)).toBe("3 votes");
    expect(weightText(null, null)).toBe("—");
  });
});

describe("isReduced", () => {
  it("is true only when less than the cast weight is left", () => {
    expect(isReduced({ weight: "10", effective_weight: "7" })).toBe(true);
    expect(isReduced({ weight: "10", effective_weight: "0" })).toBe(true);
    expect(isReduced({ weight: "10", effective_weight: "10" })).toBe(false);
  });
});

describe("summaryText", () => {
  it("is nothing for a blank summary", () => {
    expect(summaryText(null)).toBeNull();
    expect(summaryText("  \n ")).toBeNull();
  });
  it("keeps up to 5,000 characters whole", () => {
    expect(summaryText("a".repeat(SUMMARY_LIMIT))).toEqual({ text: "a".repeat(SUMMARY_LIMIT), cut: false });
  });
  it("cuts a longer one and says so", () => {
    const s = summaryText("b".repeat(SUMMARY_LIMIT + 1));
    expect(s?.cut).toBe(true);
    expect(s?.text).toHaveLength(SUMMARY_LIMIT);
  });
});
