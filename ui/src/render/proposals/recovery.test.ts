import { describe, expect, it } from "vitest";

import type { Upgrade, UpgradeMember } from "@/api/types";

import upgradeJson from "../../../test/fixtures/api/v1/upgrades/23855000.json";

import { backSpan, recoveryHeader, recoveryText } from "./recovery";

const u = upgradeJson as Upgrade;
const member = (m: Partial<UpgradeMember>): UpgradeMember => ({ ...u.members[0], ...m });

describe("backSpan", () => {
  it("says under a minute, then minutes, then hours and minutes", () => {
    expect(backSpan(0)).toBe("under a minute");
    expect(backSpan(59)).toBe("under a minute");
    expect(backSpan(60)).toBe("1 min");
    expect(backSpan(7 * 60 + 30)).toBe("7 min");
    expect(backSpan(2 * 3600 + 14 * 60)).toBe("2 h 14 min");
  });
});

describe("recoveryHeader", () => {
  it("names the block, the halt and how long the chain was down", () => {
    expect(recoveryHeader(u)).toEqual({ height: "23,855,000", halt: "Jul 30, 2026, 12:00 UTC", downtime: "2 min" });
  });

  it("is null before the halt and the restart are known", () => {
    expect(recoveryHeader({ ...u, halt_time: null, downtime_seconds: null })).toBeNull();
  });
});

describe("recoveryText", () => {
  it("times a validator the halt watch saw from the halt", () => {
    expect(recoveryText(member({ back_source: "halt_vote", seconds_back: 42 }), u)).toEqual({ text: "back in under a minute", tone: "normal" });
    expect(recoveryText(member({ back_source: "halt_vote", seconds_back: 8040 }), u).text).toBe("back in 2 h 14 min");
  });

  it("times a validator found in the commits from the restart, with the blocks it missed", () => {
    const later = member({ back_source: "commit", back_time: "2026-07-30T12:09:31Z", seconds_back: 571, blocks_needed: 70 });
    expect(recoveryText(later, u)).toEqual({ text: "back 7 min after the restart · 70 blocks", tone: "normal" });
    const next = member({ back_source: "commit", back_time: "2026-07-30T12:02:37Z", seconds_back: 157, blocks_needed: 1 });
    expect(recoveryText(next, u).text).toBe("back under a minute after the restart · 1 block");
  });

  it("says a validator that signed the first block came back with the restart", () => {
    const first = member({ back_source: "commit", back_time: u.resume_time, seconds_back: 151, blocks_needed: 0 });
    expect(recoveryText(first, { ...u, watched: false } as Upgrade).text).toBe("back when the chain restarted");
  });

  it("says who is not back: within the window once read, not yet before", () => {
    const missing = member({ back_time: null, back_source: null, seconds_back: null });
    expect(recoveryText(missing, u)).toEqual({ text: "not back within 10,000 blocks", tone: "neg" });
    expect(recoveryText(missing, { ...u, window_end_height: null })).toEqual({ text: "not back within 10,000 blocks", tone: "neg" });
    expect(recoveryText(missing, { ...u, computed_at: null })).toEqual({ text: "not back yet", tone: "muted" });
  });
});
