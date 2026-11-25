import { describe, expect, it } from "vitest";

import type { DaoEvent } from "@/api/types";

import { daoOfLabel, destinationText, durationText, fieldWords, historyLine, listAttr, parseDaoId, parseDaoTab, ratioText, refundText, typeLabel } from "./daos";

const event = (type: string, attrs: Record<string, unknown>): DaoEvent => ({
  height: 1,
  tx_index: 0,
  msg_index: 0,
  seq: 0,
  type,
  attrs,
  tx_hash: null,
  time: "2099-10-02T09:15:00Z",
});

const text = (parts: ReturnType<typeof historyLine>) => parts.map((p) => ("text" in p ? p.text : `<${p.address}>`)).join("");

describe("parseDaoId", () => {
  it("takes a positive integer and nothing else", () => {
    expect(parseDaoId("7")).toBe(7);
    expect(parseDaoId("0")).toBeNull();
    expect(parseDaoId("07")).toBeNull();
    expect(parseDaoId("1e3")).toBeNull();
    expect(parseDaoId("-1")).toBeNull();
    expect(parseDaoId("99999999999999999")).toBeNull();
  });
});

describe("words", () => {
  it("names the types", () => {
    expect(typeLabel("stake")).toBe("Stakers");
    expect(typeLabel("static_list")).toBe("Member list");
  });

  it("turns ratios into percentages", () => {
    expect(ratioText("0.400000000000000000")).toBe("40%");
    expect(ratioText("0.666")).toBe("66.6%");
    expect(ratioText("n/a")).toBe("n/a");
  });

  it("writes durations in the largest whole units", () => {
    expect(durationText(259200)).toBe("3 days");
    expect(durationText(86400)).toBe("1 day");
    expect(durationText(129600)).toBe("1 day 12 h");
    expect(durationText(7200)).toBe("2 hours");
    expect(durationText(5400)).toBe("1 hour 30 min");
    expect(durationText(120)).toBe("2 minutes");
    expect(durationText(0)).toBe("none");
  });

  it("reads the refund policy and destination enums with or without their prefix", () => {
    expect(refundText("ONLY_PASSED")).toBe("Refunded only if the proposal passes");
    expect(refundText("DEPOSIT_REFUND_POLICY_ALWAYS")).toBe("Always refunded");
    expect(refundText("never")).toBe("Never refunded");
    expect(refundText("SOMETHING")).toBe("SOMETHING");
    expect(destinationText("FUNDS_DESTINATION_BURN")).toBe("burned");
    expect(destinationText("TREASURY")).toBe("kept in the DAO's treasury");
  });

  it("names config fields in plain words and keeps unknown ones readable", () => {
    expect(fieldWords(["min_quorum", "threshold", "new_thing"])).toBe("quorum, threshold, new thing");
  });
});

describe("daoOfLabel", () => {
  it("reads the id and name of a dao label only", () => {
    expect(daoOfLabel({ name: "DAO #7 · Honey Council", kind: "dao" })).toEqual({ id: 7, name: "Honey Council" });
    expect(daoOfLabel({ name: "DAO #7", kind: "dao" })).toEqual({ id: 7, name: "" });
    expect(daoOfLabel({ name: "DAO #7 · X", kind: "known" })).toBeNull();
    expect(daoOfLabel({ name: "DAO #7 · Line one\nline two", kind: "dao" })).toEqual({ id: 7, name: "Line one\nline two" });
    expect(daoOfLabel(null)).toBeNull();
  });
});

describe("parseDaoTab", () => {
  it("falls back to Treasury", () => {
    expect(parseDaoTab("history")).toBe("history");
    expect(parseDaoTab("nope")).toBe("treasury");
    expect(parseDaoTab(undefined)).toBe("treasury");
  });
});

describe("listAttr", () => {
  it("reads arrays, JSON arrays and comma lists", () => {
    expect(listAttr(["a", "b"])).toEqual(["a", "b"]);
    expect(listAttr('["a","b"]')).toEqual(["a", "b"]);
    expect(listAttr("a, b")).toEqual(["a", "b"]);
    expect(listAttr("")).toEqual([]);
    expect(listAttr(undefined)).toEqual([]);
  });
});

describe("historyLine", () => {
  const dao = { creator: "bze1creator" };

  it("says who created the DAO, or just that it was created", () => {
    expect(text(historyLine(event("EventDaoCreated", {}), dao))).toBe("Created by <bze1creator>");
    expect(text(historyLine(event("EventDaoCreated", {}), { creator: null }))).toBe("Created");
  });

  it("writes the admin changes", () => {
    expect(text(historyLine(event("EventAdminChanged", { old_admin: "bze1a", new_admin: "bze1b" }), dao))).toBe(
      "Admin changed from <bze1a> to <bze1b>",
    );
    expect(text(historyLine(event("EventAdminNominated", { nominee: "bze1b" }), dao))).toBe("Admin handover offered to <bze1b>");
    expect(text(historyLine(event("EventAdminNominationWithdrawn", { nominee: "bze1b" }), dao))).toBe("Admin handover to <bze1b> withdrawn");
    expect(text(historyLine(event("EventAdminExecuted", { msg_count: "2" }), dao))).toBe("Admin ran 2 messages as the DAO");
    expect(text(historyLine(event("EventAdminExecuted", { msg_count: "1" }), dao))).toBe("Admin ran 1 message as the DAO");
  });

  it("writes members, rules, metadata and electorate changes", () => {
    expect(text(historyLine(event("EventDaoMembersUpdated", { added: ["bze1a", "bze1b"], removed: ["bze1c"] }), dao))).toBe(
      "Members added: <bze1a>, <bze1b> · Members removed: <bze1c>",
    );
    expect(text(historyLine(event("EventDaoConfigUpdated", { fields: ["min_quorum", "threshold"] }), dao))).toBe("Rules changed: quorum, threshold");
    expect(text(historyLine(event("EventDaoConfigUpdated", {}), dao))).toBe("Rules changed");
    expect(text(historyLine(event("EventDaoMetadataUpdated", {}), dao))).toBe("Name and description changed");
    expect(text(historyLine(event("EventDaoElectorateUpdated", { sr_ids: ["3"], dr_denom: "uhoney" }), dao))).toBe(
      "Voters changed: now stake in staking rewards 3 and the denom reward of uhoney",
    );
  });

  it("keeps an event it does not know readable", () => {
    expect(text(historyLine(event("EventFutureThing", {}), dao))).toBe("Future Thing");
  });
});
