import BigNumber from "bignumber.js";

import type { Halted, LiveHaltMember, Status } from "@/api/types";
import { formatCount, formatPct } from "@/lib/format";
import { durationText } from "@/render/future-block";

// The chain's halt for a software upgrade, and the explorer falling behind, in
// plain words. Pure functions over `GET /upgrades/live` and `GET /status`.

/** The share of the voting power consensus needs to decide a block: more than two thirds. */
export const NEEDED_PCT = new BigNumber(200).div(3);

/** `power` as a percentage of `total`, unrounded; 0 when the total is 0. */
export function sharePct(power: string, total: string): BigNumber {
  const t = new BigNumber(total);
  return t.gt(0) ? new BigNumber(power).times(100).div(t) : new BigNumber(0);
}

export interface HaltProgress {
  /** Shares of the voting power, percentages. */
  back: BigNumber;
  prevote: BigNumber;
  precommit: BigNumber;
  /** How much more voting power must come back, a percentage; 0 once enough is back. */
  short: BigNumber;
}

export function haltProgress(h: Halted): HaltProgress {
  const back = sharePct(h.back_power, h.total_power);
  const short = NEEDED_PCT.minus(back);
  return {
    back,
    prevote: sharePct(h.prevote_power, h.total_power),
    precommit: sharePct(h.precommit_power, h.total_power),
    short: short.gt(0) ? short : new BigNumber(0),
  };
}

/** The validators not back yet, by voting power (the API's order). */
export function missingMembers(h: Halted): LiveHaltMember[] {
  return h.members.filter((m) => !m.back);
}

/** `58.0% back, 8.7% more needed` or `67.2% back, enough to restart`. */
export function backLine(p: HaltProgress): string {
  const back = `${formatPct(p.back.toString())} of the voting power is back`;
  return p.short.gt(0) ? `${back}, ${formatPct(p.short.toString())} more is needed` : `${back}, enough to restart`;
}

/** How long the chain has been halted at `now`: `under a minute`, `12 min`. */
export function haltedFor(h: Pick<Halted, "halted_since">, now: Date): string {
  const ms = now.getTime() - new Date(h.halted_since).getTime();
  return ms < 60_000 ? "under a minute" : durationText(ms);
}

/**
 * The banner's line while the explorer is behind the chain (never an upgrade's
 * halt): how far, in blocks, when the node's height is known; null while it
 * keeps up.
 */
export function behindLine(s: Status | undefined): string | null {
  if (!s || s.live_fill.healthy) return null;
  const { db_height: db, node_height: node } = s.live_fill;
  if (db != null && node != null && node > db) {
    const n = node - db;
    return `The explorer is ${formatCount(n)} block${n === 1 ? "" : "s"} behind the chain. The chain itself is running; the latest blocks and transactions will show up here shortly.`;
  }
  return "The explorer is not keeping up with the chain right now. The chain itself is not affected; the latest blocks and transactions may be missing here for a moment.";
}
