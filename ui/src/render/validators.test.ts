import { describe, expect, it } from "vitest";

import type { Validator, ValidatorDetail, ValidatorEvent } from "@/api/types";

import detail from "../../test/fixtures/api/v1/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c.json";
import list from "../../test/fixtures/api/v1/validators.json";
import params from "../../test/fixtures/api/v1/params.json";

import {
  cleanRecord,
  eventText,
  filterByMoniker,
  historyItems,
  pointsText,
  proposalFrequency,
  rateText,
  rewardRateAfterCommission,
  signedBlocksWindow,
  splitValidators,
  statusLine,
  unbondingText,
  uptimeDetail,
  uptimeText,
  voteOption,
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
  });
});
