import type { Upgrade, UpgradeMember } from "@/api/types";
import { formatCount, formatDateTime } from "@/lib/format";
import { durationText } from "@/render/future-block";

// When each validator came back after a software upgrade halted the chain, in
// plain words. Pure functions over `GET /upgrades/{height}`.

/** How many blocks after the halt the explorer reads signatures for when it does not say. */
export const DEFAULT_RECOVERY_WINDOW = 10_000;

/** A span of seconds: `under a minute`, `7 min`, `2 h 14 min`. */
export function backSpan(seconds: number): string {
  return seconds < 60 ? "under a minute" : durationText(seconds * 1000);
}

/** `The chain halted at block U on {date} and restarted 12 min later.`; null before the halt and the restart are known. */
export function recoveryHeader(u: Pick<Upgrade, "height" | "halt_time" | "downtime_seconds">): { height: string; halt: string; downtime: string } | null {
  if (u.halt_time == null || u.downtime_seconds == null) return null;
  return { height: formatCount(u.height), halt: formatDateTime(u.halt_time, { seconds: false }), downtime: backSpan(u.downtime_seconds) };
}

export type RecoveryTone = "normal" | "muted" | "neg";

/**
 * A member's line: `back in 4 min` (seen by the halt watch), `back 2 min after
 * the restart · 18 blocks`, `back when the chain restarted` (signed the first
 * block), `not back within 10,000 blocks` once the window is read, and
 * `not back yet` while it is still being read.
 */
export function recoveryText(m: UpgradeMember, u: Pick<Upgrade, "height" | "resume_time" | "window_end_height" | "computed_at">): { text: string; tone: RecoveryTone } {
  if (m.back_time == null) {
    if (u.computed_at == null) return { text: "not back yet", tone: "muted" };
    const window = u.window_end_height != null ? u.window_end_height - u.height : DEFAULT_RECOVERY_WINDOW;
    return { text: `not back within ${formatCount(window)} blocks`, tone: "neg" };
  }
  if (m.back_source === "halt_vote") {
    return { text: `back in ${backSpan(m.seconds_back ?? 0)}`, tone: "normal" };
  }
  const blocks = m.blocks_needed ?? 0;
  if (blocks <= 0 || u.resume_time == null) return { text: "back when the chain restarted", tone: "normal" };
  const after = Math.max(0, Math.round((Date.parse(m.back_time) - Date.parse(u.resume_time)) / 1000));
  return { text: `back ${backSpan(after)} after the restart · ${formatCount(blocks)} ${blocks === 1 ? "block" : "blocks"}`, tone: "normal" };
}
