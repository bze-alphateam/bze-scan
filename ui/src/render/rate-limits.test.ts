import { describe, expect, it } from "vitest";

import type { RateLimit, RateLimitDirection } from "@/api/types";

import { directionView, limitsByChannel, resetText } from "./rate-limits";

import limitsJson from "../../test/fixtures/api/v1/ibc/rate-limits.json";

const [ubze] = limitsJson.items as RateLimit[];
const direction = (patch: Partial<RateLimitDirection>): RateLimitDirection => ({ ...ubze.send, ...patch });
const limit = (send: Partial<RateLimitDirection>, recv: Partial<RateLimitDirection> = {}): RateLimit => ({
  ...ubze,
  send: direction(send),
  recv: direction(recv),
});

describe("directionView", () => {
  it("shows the used part of the threshold at 60% used", () => {
    expect(directionView(ubze, "send")).toMatchObject({
      kind: "bar",
      label: "Out",
      text: "1.2M of 2M BZE used (60%)",
      pct: 60,
      tone: "ok",
      note: null,
    });
  });

  it("turns the bar orange from 75% and red from 90%", () => {
    const tones = [0, 74.9, 75, 89.9, 90, 100].map((p) => (directionView(limit({ used_pct: p }), "send") as { tone: string }).tone);
    expect(tones).toEqual(["ok", "ok", "warn", "warn", "bad", "bad"]);
  });

  it("refuses an enforced direction whose threshold rounds to zero instead of showing 0 of 0", () => {
    const view = directionView(limit({ max_percent: "2", threshold: "0", used_pct: null, enforced: true, remaining: "0" }), "send");
    expect(view).toMatchObject({ kind: "blocked", text: "The limit rounds to zero: no transfers this way" });
  });

  it("shows an empty bar at 0% used", () => {
    expect(directionView(limit({ used_pct: 0, net_flow: "0", used: "0" }), "send")).toMatchObject({ pct: 0, tone: "ok", text: "0 of 2M BZE used (0%)" });
  });

  it("shows a full red bar at 100% used", () => {
    expect(directionView(limit({ used_pct: 100, net_flow: "2000000000000", used: "2000000000000" }), "send")).toMatchObject({ pct: 100, tone: "bad", text: "2M of 2M BZE used (100%)" });
  });

  it("floors a negative net flow at 0% and says there is room above the limit", () => {
    const view = directionView(limit({}, { net_flow: "-300000000000", used: "0", used_pct: 0 }), "recv");
    expect(view).toMatchObject({ kind: "bar", label: "In", pct: 0, note: "net inflow 300K BZE, room above the limit" });
  });

  it("says a 0% quota does not allow the direction", () => {
    expect(directionView(limit({}, { max_percent: "0", threshold: "0", used_pct: null }), "recv")).toEqual({
      kind: "blocked",
      label: "In",
      text: "Incoming transfers of this token are not allowed on this route",
    });
    expect(directionView(limit({ max_percent: "0.0" }), "send")).toMatchObject({ kind: "blocked", text: "Outgoing transfers of this token are not allowed on this route" });
  });

  it("says a 100% quota has no practical limit", () => {
    expect(directionView(limit({ max_percent: "100" }), "send")).toEqual({ kind: "unlimited", label: "Out", text: "No practical limit" });
  });

  it("says when the module does not enforce (no supply)", () => {
    expect(directionView(limit({ enforced: false, used_pct: null }), "send").kind).toBe("unenforced");
  });

  it("reads an unenforced limit as not enforced, whatever its quota or remaining", () => {
    const unenforced = { enforced: false, remaining: "0", used_pct: null };
    expect(directionView(limit({ ...unenforced, max_percent: "0" }), "send").kind).toBe("unenforced");
    expect(directionView(limit({ ...unenforced, max_percent: "100" }), "send").kind).toBe("unenforced");
  });

  it("falls back to the denom's last segment without a symbol", () => {
    expect(directionView({ ...limit({}), symbol: null, denom: "factory/bze1x/uhoney" }, "send")).toMatchObject({ text: expect.stringContaining("uhoney") });
  });
});

describe("resetText", () => {
  const now = new Date("2099-10-11T18:48:00Z");
  it("counts down to the next reset with its UTC clock time", () => {
    expect(resetText({ next_reset: "2099-10-12T00:00:00Z", window_hours: 24 }, now)).toBe("resets in 5 h 12 m (00:00 UTC)");
  });
  it("counts minutes alone under an hour", () => {
    expect(resetText({ next_reset: "2099-10-11T19:05:00Z", window_hours: 24 }, now)).toBe("resets in 17 m (19:05 UTC)");
  });
  it("says a reset in the past is happening now", () => {
    expect(resetText({ next_reset: "2099-10-11T18:00:00Z", window_hours: 24 }, now)).toBe("resetting now");
  });
  it("names the window without a clock time when the next reset is unknown", () => {
    expect(resetText({ next_reset: null, window_hours: 7 }, now)).toBe("resets every 7 h");
  });
});

describe("limitsByChannel", () => {
  it("groups by channel in the answer's order", () => {
    const grouped = limitsByChannel(limitsJson.items as RateLimit[]);
    expect([...grouped.keys()]).toHaveLength(8);
    expect(grouped.get("channel-3")?.map((l) => l.symbol)).toEqual(["BZE", "USDC.n"]);
  });
});
