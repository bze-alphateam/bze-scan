import { describe, expect, it } from "vitest";

import type { ChainParams, StakeChanges, Validator, ValidatorDetail, ValidatorEvent } from "@/api/types";

import detail from "../../test/fixtures/api/v1/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c.json";
import list from "../../test/fixtures/api/v1/validators.json";
import params from "../../test/fixtures/api/v1/params.json";
import changes from "../../test/fixtures/api/v1/validators/stake-changes.json";

import {
  downtimeSlashText,
  fameMissedText,
  jailedInText,
  onlineLines,
  onlineState,
  uptimeTone,
  recentVotesText,
  signingText,
  cleanRecord,
  eventText,
  filterByMoniker,
  historyItems,
  movementAmount,
  movementLabel,
  movementsRange,
  pointsText,
  proposalFrequency,
  rateText,
  rewardRateAfterCommission,
  signedBlocksWindow,
  signedBondCompact,
  splitValidators,
  stakeChangeLines,
  statusLine,
  unbondingText,
  uptimeDetail,
  uptimeText,
  voteOption,
  voteTone,
} from "./validators";

const v = detail as unknown as ValidatorDetail;
const event = (kind: string, details: Record<string, unknown> | null = null, height = 10): ValidatorEvent => ({
  height,
  tx_index: -1,
  tx_hash: null,
  kind,
  details,
  time: "2026-01-02T00:00:00Z",
});

describe("splitValidators and filterByMoniker", () => {
  it("splits the bonded set from the rest, keeping the API's order", () => {
    const { active, inactive } = splitValidators(list.items as Validator[]);
    expect(active.map((x) => x.moniker)).toEqual(["Honeycomb", "Thamar Node", "Deep Fields"]);
    expect(inactive.map((x) => x.moniker)).toEqual(["Sleepy Node", "Old Timer"]);
  });

  it("filters on the moniker in any case, all for a blank query", () => {
    expect(filterByMoniker(list.items, "  NODE ").map((x) => x.moniker)).toEqual(["Thamar Node", "Sleepy Node"]);
    expect(filterByMoniker(list.items, " ")).toHaveLength(5);
    expect(filterByMoniker(list.items, "nobody")).toEqual([]);
  });
});

describe("rates", () => {
  it("takes the commission off the base reward rate", () => {
    expect(rewardRateAfterCommission("8.9", "0.05")).toBe("8.455");
    expect(rewardRateAfterCommission("10", "0")).toBe("10");
    expect(rewardRateAfterCommission("10", "1")).toBe("0");
    expect(rewardRateAfterCommission(null, "0.05")).toBeNull();
  });

  it("formats fractions, percentages and uptime with at most one decimal", () => {
    expect(rateText("0.05")).toBe("5%");
    expect(rateText("0.045")).toBe("4.5%");
    expect(rateText(null)).toBe("—");
    expect(uptimeText("100.00000")).toBe("100%");
    expect(uptimeText("99.99000")).toBe("99.9%");
    expect(uptimeText(null)).toBe("—");
    expect(pointsText("0.05")).toBe("5 points");
    expect(pointsText("0.01")).toBe("1 point");
  });

  it("says how often a validator proposes and how long unstaking takes", () => {
    expect(proposalFrequency("12.34567")).toBe("1 in every 8 blocks, on average");
    expect(proposalFrequency(null)).toBeNull();
    expect(unbondingText(1_814_400)).toBe("21 days");
    expect(unbondingText(null)).toBeNull();
  });

  it("reads the signing window from the parameters", () => {
    expect(signedBlocksWindow(params as never)).toBe(10_000);
    expect(signedBlocksWindow(undefined)).toBeNull();
    expect(signedBlocksWindow({ slashing: { params: {}, last_change: null } })).toBeNull();
  });

  it("describes the uptime from the missed blocks", () => {
    expect(uptimeDetail(0, 10_000)).toBe("signed all of the last 10,000 blocks");
    expect(uptimeDetail(160, 10_000)).toBe("missed 160 of the last 10,000 blocks");
    expect(uptimeDetail(3, null)).toBeNull();
  });
});

describe("statusLine", () => {
  const now = new Date("2026-10-11T12:00:00Z");
  it("names the rank in the active set", () => {
    expect(statusLine(v, 22, now)).toBe("Active · rank 1 of 22");
    expect(statusLine(v, null, now)).toBe("Active · rank 1");
  });

  it("puts tombstoned before jailed before inactive", () => {
    expect(statusLine({ ...v, status: "unbonded", rank: null }, 22, now)).toBe("Inactive");
    expect(statusLine({ ...v, jailed: true, jailed_until: "2026-10-12T00:00:00Z" }, 22, now)).toBe(
      "Jailed until Oct 12, 2026, 00:00:00 UTC",
    );
    expect(statusLine({ ...v, jailed: true, jailed_until: "2026-10-01T00:00:00Z" }, 22, now)).toBe("Jailed");
    expect(statusLine({ ...v, jailed: true, tombstoned: true }, 22, now)).toBe("Tombstoned");
  });
});

describe("history", () => {
  it("lists the fixture's events newest first in plain words", () => {
    expect(historyItems(v.events)).toEqual([
      expect.objectContaining({ text: "Commission changed from 4% to 5%", date: "Mar 12, 2025", txHash: expect.stringMatching(/^9C1E/) }),
      expect.objectContaining({ text: "Joined the active set", date: "Apr 17, 2022", txHash: null }),
      expect.objectContaining({ text: "Created with 4% commission", date: "Apr 17, 2022" }),
    ]);
  });

  it("orders by height whatever the input order", () => {
    const items = historyItems([event("unjailed", null, 5), event("jailed", null, 9)]);
    expect(items.map((h) => h.text)).toEqual(["Jailed: stopped producing blocks", "Unjailed: back to producing blocks"]);
  });

  it("names every kind the backend writes", () => {
    expect(eventText(event("description_changed", { to: { moniker: "Hive" } }))).toBe("Renamed to Hive");
    expect(eventText(event("description_changed", { to: { website: "x" } }))).toBe("Profile updated");
    expect(eventText(event("commission_changed", { to: "0.07" }))).toBe("Commission changed to 7%");
    expect(eventText(event("slashed", { reason: "missing_signature" }))).toBe("Penalised for missing too many blocks");
    expect(eventText(event("slashed", { reason: "double_sign" }))).toBe("Penalised for signing two blocks at one height");
    expect(eventText(event("slashed"))).toBe("Penalised");
    expect(eventText(event("tombstoned"))).toBe("Tombstoned: removed for good");
    expect(eventText(event("unbonded"))).toBe("Left the active set");
    expect(eventText(event("something_new"))).toBe("Something new");
  });

  it("says never jailed only when no jail, slash or tombstone is on record", () => {
    expect(cleanRecord(v)).toBe("Never jailed, never penalised since Apr 17, 2022");
    expect(cleanRecord({ ...v, first_seen_time: null })).toBe("Never jailed, never penalised");
    expect(cleanRecord({ ...v, events: [event("slashed")] })).toBeNull();
    expect(cleanRecord({ ...v, events: [event("jailed")] })).toBeNull();
  });

  it("words a vote", () => {
    expect(voteOption({ option: "yes" })).toBe("Yes");
    expect(voteOption({ option: "no_with_veto" })).toBe("No with veto");
    expect(voteOption({ option: null })).toBe("Split vote");
    expect(["yes", "no", "abstain", "no_with_veto", null].map((option) => voteTone({ option }))).toEqual([
      "green",
      "red",
      "gray",
      "orange",
      "purple",
    ]);
  });
});

describe("hall of fame", () => {
  it("words the crown card's signing line with the rule's allowance", () => {
    const rules = { proposals: 10, max_missed_blocks: 10, signed_blocks_window: 10_000 };
    expect(fameMissedText({ missed_blocks: 4, signed_blocks_window: 5 }, rules)).toBe("Missed 4 of the last 10,000 blocks (10 allowed)");
    expect(fameMissedText({ missed_blocks: 0, signed_blocks_window: null }, rules)).toBe("Missed no blocks in the last 10,000 (10 allowed)");
    // The rule's window first, the validator's while the rule's is unknown.
    const noWindow = { ...rules, signed_blocks_window: null };
    expect(fameMissedText({ missed_blocks: 2, signed_blocks_window: 20_000 }, noWindow)).toBe("Missed 2 of the last 20,000 blocks (10 allowed)");
    expect(fameMissedText({ missed_blocks: 2, signed_blocks_window: null }, { ...noWindow, max_missed_blocks: 5 })).toBe(
      "Missed 2 of the window's blocks (5 allowed)",
    );
  });

  it("tones an uptime: red under 99.5%, muted at 100%", () => {
    expect(uptimeTone("99.40000")).toBe("bad");
    expect(uptimeTone("99.90000")).toBeNull();
    expect(uptimeTone("100.00000")).toBe("muted");
    expect(uptimeTone(null)).toBeNull();
  });

  it("says how it signed and voted", () => {
    expect(signingText({ missed_blocks: 0, signed_blocks_window: 10000 }, 5)).toBe("Didn't miss a block in the last 10,000 blocks");
    expect(signingText({ missed_blocks: 3, signed_blocks_window: null }, 10000)).toBe("Missed 3 of the last 10,000 blocks");
    expect(signingText({ missed_blocks: null, signed_blocks_window: null }, 10000)).toBe("Signing record not known yet");
    expect(recentVotesText({ recent_votes: { voted: 10, of: 10 } })).toBe("Voted on all of the last 10 proposals");
    expect(recentVotesText({ recent_votes: { voted: 1, of: 1 } })).toBe("Voted on the last proposal");
    expect(recentVotesText({ recent_votes: { voted: 4, of: 10 } })).toBe("Voted on 4 of the last 10 proposals");
    expect(recentVotesText({ recent_votes: { voted: 0, of: 0 } })).toBe("No proposal has closed yet");
  });
});

describe("online status", () => {
  const p = params as ChainParams;

  it("is unknown outside the active set", () => {
    expect(onlineState({ offline: null })).toBeNull();
    expect(onlineState({ offline: false })).toBe("online");
    expect(onlineState({ offline: true })).toBe("offline");
  });

  it("words the card", () => {
    expect(onlineLines({ offline: false, missed_in_a_row: 0, last_signed_height: 100 })).toEqual(["Signed the latest block, 100."]);
    expect(onlineLines({ offline: false, missed_in_a_row: 1, last_signed_height: 1000 })).toEqual(["Signed block 1,000, 1 block ago."]);
    expect(onlineLines({ offline: false, missed_in_a_row: 3, last_signed_height: 1000 })).toEqual(["Signed block 1,000, 3 blocks ago."]);
    expect(onlineLines({ offline: true, missed_in_a_row: 600, last_signed_height: 1000 }, 6000)).toEqual([
      "Missed the last 600 blocks in a row, about 1 hour.",
      "Last signed block 1,000.",
    ]);
    expect(onlineLines({ offline: true, missed_in_a_row: 12, last_signed_height: null })).toEqual([
      "Missed the last 12 blocks in a row.",
      "Not seen signing since the explorer started watching.",
    ]);
  });

  it("estimates the jail from the slashing parameters", () => {
    // 5% of 10,000 must be signed: jailed past 9,500 missed.
    expect(jailedInText({ missed_blocks: 9000, signed_blocks_window: 10000 }, p, 6000)).toBe("about 50 minutes");
    expect(jailedInText({ missed_blocks: 9500, signed_blocks_window: 10000 }, p, 6000)).toBe("any moment now");
    expect(jailedInText({ missed_blocks: null, signed_blocks_window: 10000 }, p, 6000)).toBeNull();
    expect(jailedInText({ missed_blocks: 9000, signed_blocks_window: 10000 }, undefined, 6000)).toBeNull();
    expect(jailedInText({ missed_blocks: 9000, signed_blocks_window: 10000 }, p, null)).toBeNull();
    expect(downtimeSlashText(p)).toBe("0.01%");
    expect(downtimeSlashText(undefined)).toBeNull();
  });
});

describe("stake changes", () => {
  const all = changes as StakeChanges;

  it("formats a signed change compactly, sign always shown", () => {
    expect(signedBondCompact("122000000000")).toBe("+122.0K");
    expect(signedBondCompact("-1640000000000")).toBe("\u22121.64M");
    expect(signedBondCompact("-2925000000000")).toBe("\u22122.93M");
    expect(signedBondCompact("1500000000000000")).toBe("+1.50B");
    expect(signedBondCompact("250000000")).toBe("+250");
    expect(signedBondCompact("-500000")).toBe("\u22120.5");
    expect(signedBondCompact("3")).toBe("+<0.01");
  });

  it("shows growth and shrink per window, and never a zero window", () => {
    expect(stakeChangeLines(all, "bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c")).toEqual([
      { window: "24h", up: true, text: "+122.0K" },
      { window: "7d", up: true, text: "+185.0K" },
    ]);
    expect(stakeChangeLines(all, "bzevaloper1x7ym3n6d9kjwz0q4u5r2a8l4tqs9vfmyh2gz3e")).toEqual([
      { window: "24h", up: false, text: "\u22121.64M" },
      { window: "7d", up: false, text: "\u22122.93M" },
    ]);
    expect(stakeChangeLines(all, "bzevaloper1c0673y65z89fuqmw3jhhx6a3gvs67wtuw9l440")).toEqual([{ window: "7d", up: true, text: "+48.5K" }]);
    // Both windows at 0, and a validator without movements: nothing.
    expect(stakeChangeLines(all, "bzevaloper12ngv3tq3fqaevdlu5hznemz5m999l2t8zpggf8")).toEqual([]);
    expect(stakeChangeLines(all, "bzevaloper1jr90yqzvqr5g894qxqly4vaae3u032wlljd0ev")).toEqual([]);
  });

  it("shows nothing while the window is not fully indexed, or not loaded", () => {
    expect(stakeChangeLines({ ...all, complete: false }, "bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c")).toEqual([]);
    expect(stakeChangeLines(undefined, "bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c")).toEqual([]);
  });
});

describe("stake movements", () => {
  const cp = (moniker: string | null) => ({ operator_address: "bzevaloper1x7ym3n6d9kjwz0q4u5r2a8l4tqs9vfmyh2gz3e", moniker });

  it("names every kind, and the other validator of a redelegation", () => {
    expect(movementLabel({ kind: "delegate", counterparty_validator: null })).toEqual({ text: "Delegated", counterparty: null });
    expect(movementLabel({ kind: "undelegate", counterparty_validator: null }).text).toBe("Undelegated");
    expect(movementLabel({ kind: "cancel_unbonding", counterparty_validator: null }).text).toBe("Cancelled unbonding");
    expect(movementLabel({ kind: "create", counterparty_validator: null }).text).toBe("Initial self-stake");
    expect(movementLabel({ kind: "redelegate_out", counterparty_validator: cp("Thamar Node") })).toEqual({
      text: "Moved to",
      counterparty: { operator: cp(null).operator_address, name: "Thamar Node" },
    });
    // An unknown validator goes by its short address.
    expect(movementLabel({ kind: "redelegate_in", counterparty_validator: cp(null) })).toEqual({
      text: "Moved from",
      counterparty: { operator: cp(null).operator_address, name: "bzevaloper1x7y…gz3e" },
    });
    expect(movementLabel({ kind: "redelegate_in", counterparty_validator: null }).text).toBe("Moved from another validator");
  });

  it("signs the amount by its direction", () => {
    expect(movementAmount({ kind: "delegate", amount: "120000000000" })).toBe("+120,000 BZE");
    expect(movementAmount({ kind: "cancel_unbonding", amount: "2000000" })).toBe("+2 BZE");
    expect(movementAmount({ kind: "redelegate_out", amount: "40000000000" })).toBe("\u221240,000 BZE");
    expect(movementAmount({ kind: "undelegate", amount: "7500000" })).toBe("\u22127.5 BZE");
  });

  it("says which rows a page shows", () => {
    expect(movementsRange(1, 10, 12)).toBe("Movements 1–10 of 12");
    expect(movementsRange(2, 10, 12)).toBe("Movements 11–12 of 12");
    expect(movementsRange(1, 10, 0)).toBe("Movements 0 of 0");
    expect(movementsRange(3, 10, 12)).toBe("Movements 0 of 12");
  });
});
