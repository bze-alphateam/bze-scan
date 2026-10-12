import BigNumber from "bignumber.js";

import type { ChainParams, Validator, ValidatorDetail, ValidatorEvent, ValidatorVote } from "@/api/types";
import { formatAmount, formatCount, formatDate, formatDateTime, formatPct } from "@/lib/format";
import { compareDecimals } from "@/lib/math";

// The validators pages' words and numbers, kept pure so they are tested
// without rendering. Rates from the API: `commission_*` are fractions
// ("0.05"), `voting_power_pct`, `uptime` and the summary's shares are
// percentages ("12.34567").

/** The bond denom's display: every staking amount is `ubze`. */
export const BOND = { exponent: 6, symbol: "BZE" } as const;

/** `14.3M BZE`. */
export const bondCompact = (base: string) => formatAmount(base, BOND.exponent, BOND.symbol, { compact: true });

/** `120,000 BZE`. */
export const bondFull = (base: string) => formatAmount(base, BOND.exponent, BOND.symbol);

/** The active set (bonded) and the rest, each in the API's order. */
export function splitValidators<T extends Pick<Validator, "status">>(items: T[]): { active: T[]; inactive: T[] } {
  return {
    active: items.filter((v) => v.status === "bonded"),
    inactive: items.filter((v) => v.status !== "bonded"),
  };
}

/** The most blocks a Hall of Fame validator may have missed in the signing window (99.9% of 10,000). */
export const HALL_OF_FAME_MAX_MISSED = 10;

/**
 * The Hall of Fame: active validators that voted, whatever the option, on
 * every one of the last proposals (ten, fewer while the chain has fewer)
 * and missed at most HALL_OF_FAME_MAX_MISSED blocks of the signing window;
 * the smallest first, so the stake can spread.
 */
export function hallOfFame<T extends Pick<Validator, "status" | "jailed" | "recent_votes" | "missed_blocks" | "tokens">>(items: T[]): T[] {
  return items
    .filter(
      (v) =>
        v.status === "bonded" &&
        !v.jailed &&
        v.recent_votes.of > 0 &&
        v.recent_votes.voted >= v.recent_votes.of &&
        v.missed_blocks != null &&
        v.missed_blocks <= HALL_OF_FAME_MAX_MISSED,
    )
    .sort((a, b) => compareDecimals(a.tokens, b.tokens));
}

/** What earns a validator its place in the Hall of Fame, in one sentence. */
export function hallOfFameExplainer(recentProposals: number, window: number): string {
  return (
    `In the Hall of Fame: it voted on every one of the last ${formatCount(recentProposals)} proposals and missed no more than ` +
    `${formatCount(HALL_OF_FAME_MAX_MISSED)} of the last ${formatCount(window)} blocks.`
  );
}

/** "Didn't miss a block in the last 10,000 blocks", or how many it missed. */
export function signingText(v: Pick<Validator, "missed_blocks" | "signed_blocks_window">, fallbackWindow: number): string {
  const window = formatCount(v.signed_blocks_window ?? fallbackWindow);
  if (v.missed_blocks == null) return "Signing record not known yet";
  if (v.missed_blocks === 0) return `Didn't miss a block in the last ${window} blocks`;
  return `Missed ${formatCount(v.missed_blocks)} of the last ${window} blocks`;
}

/** "Voted on all of the last 10 proposals", or on how many of them. */
export function recentVotesText(v: Pick<Validator, "recent_votes">): string {
  const { voted, of } = v.recent_votes;
  if (of === 0) return "No proposal has closed yet";
  if (voted >= of) return of === 1 ? "Voted on the last proposal" : `Voted on all of the last ${formatCount(of)} proposals`;
  return `Voted on ${formatCount(voted)} of the last ${formatCount(of)} proposals`;
}

/** The validators whose moniker contains `query`, any case; all of them for a blank query. */
export function filterByMoniker<T extends Pick<Validator, "moniker">>(items: T[], query: string): T[] {
  const q = query.trim().toLowerCase();
  return q === "" ? items : items.filter((v) => v.moniker.toLowerCase().includes(q));
}

/** A fraction as a percentage: `0.05` → `5%`. */
export function rateText(fraction: string | null | undefined): string {
  if (fraction == null) return "—";
  const v = new BigNumber(fraction);
  return v.isFinite() ? formatPct(v.times(100).toString(), { trim: true }) : "—";
}

/** A percentage value: `12.34567` → `12.3%`. */
export function pctText(pct: string | null | undefined): string {
  return pct == null ? "—" : formatPct(pct, { trim: true }) || "—";
}

/** Uptime: `100%` only when every block was signed, so it rounds down. */
export function uptimeText(uptime: string | null | undefined): string {
  return uptime == null ? "—" : formatPct(uptime, { trim: true, round: "down" }) || "—";
}

/** The commission's daily change limit in points: `0.05` → `5 points`, `0.01` → `1 point`. */
export function pointsText(fraction: string): string {
  const v = new BigNumber(fraction).times(100);
  if (!v.isFinite()) return fraction;
  const n = v.decimalPlaces(1, BigNumber.ROUND_HALF_UP).toString();
  return `${n} ${n === "1" ? "point" : "points"}`;
}

/**
 * The yearly reward a delegator earns here: the base rate (a percentage)
 * minus the validator's commission (a fraction). Null when either is unknown.
 */
export function rewardRateAfterCommission(rewardRate: string | null | undefined, commission: string): string | null {
  if (rewardRate == null) return null;
  const v = new BigNumber(rewardRate).times(new BigNumber(1).minus(commission));
  return v.isFinite() ? v.toString() : null;
}

/** `21 days` from the unbonding period in seconds. */
export function unbondingText(seconds: number | null | undefined): string | null {
  if (seconds == null) return null;
  const days = Math.round(seconds / 86_400);
  return `${days} ${days === 1 ? "day" : "days"}`;
}

/** How often the validator proposes, on average: `1 in every 8 blocks`. */
export function proposalFrequency(votingPowerPct: string | null): string | null {
  if (votingPowerPct == null) return null;
  const pct = Number(votingPowerPct);
  if (!(pct > 0)) return null;
  return `1 in every ${formatCount(Math.max(1, Math.round(100 / pct)))} blocks, on average`;
}

/** The slashing module's `signed_blocks_window`, null when the parameters are not known. */
export function signedBlocksWindow(params: ChainParams | undefined): number | null {
  const raw = params?.slashing?.params?.signed_blocks_window;
  const n = Number(raw);
  return raw != null && Number.isFinite(n) && n > 0 ? n : null;
}

/** The validator's standing, as the header's status line says it. */
export function statusLine(v: ValidatorDetail, activeCount: number | null, now: Date = new Date()): string {
  if (v.tombstoned) return "Tombstoned";
  if (v.jailed) {
    const until = v.jailed_until && new Date(v.jailed_until) > now ? v.jailed_until : null;
    return until ? `Jailed until ${formatDateTime(until)}` : "Jailed";
  }
  if (v.status === "bonded") {
    const rank = v.rank == null ? "" : ` · rank ${v.rank}${activeCount ? ` of ${activeCount}` : ""}`;
    return `Active${rank}`;
  }
  return v.status === "unbonding" ? "Unbonding" : "Inactive";
}

/** An inactive row's standing in place of its rank. */
export function inactiveLabel(status: Validator["status"]): string {
  return status === "unbonding" ? "Unbonding" : "Not active";
}

/** The uptime tile's sub-line from the signing window. */
export function uptimeDetail(missed: number | null, window: number | null): string | null {
  if (window == null) return null;
  const w = formatCount(window);
  if (!missed) return `signed all of the last ${w} blocks`;
  return `missed ${formatCount(missed)} of the last ${w} blocks`;
}

const str = (o: Record<string, unknown> | null, key: string): string | null => {
  const v = o?.[key];
  return typeof v === "string" && v !== "" ? v : null;
};

const SLASH_REASONS: Record<string, string> = {
  missing_signature: "for missing too many blocks",
  double_sign: "for signing two blocks at one height",
};

const humanise = (s: string) => s.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());

/** What a validator event says in plain words. */
export function eventText(e: Pick<ValidatorEvent, "kind" | "details">): string {
  const d = e.details;
  switch (e.kind) {
    case "created": {
      const rate = str(d, "commission_rate");
      return rate ? `Created with ${rateText(rate)} commission` : "Created";
    }
    case "commission_changed": {
      const from = str(d, "from");
      const to = str(d, "to");
      if (from && to) return `Commission changed from ${rateText(from)} to ${rateText(to)}`;
      return to ? `Commission changed to ${rateText(to)}` : "Commission changed";
    }
    case "description_changed": {
      const to = d?.to && typeof d.to === "object" ? (d.to as Record<string, unknown>) : null;
      const moniker = str(to, "moniker");
      return moniker ? `Renamed to ${moniker}` : "Profile updated";
    }
    case "unjailed":
      return "Unjailed: back to producing blocks";
    case "slashed": {
      const reason = str(d, "reason");
      const why = reason ? SLASH_REASONS[reason] ?? `(${reason.replace(/_/g, " ")})` : null;
      return why ? `Penalised ${why}` : "Penalised";
    }
    case "jailed":
      return "Jailed: stopped producing blocks";
    case "tombstoned":
      return "Tombstoned: removed for good";
    case "bonded":
      return "Joined the active set";
    case "unbonded":
      return "Left the active set";
    default:
      return humanise(e.kind);
  }
}

export interface HistoryItem {
  key: string;
  text: string;
  date: string;
  txHash: string | null;
}

/** The History card's rows, newest first. */
export function historyItems(events: ValidatorEvent[]): HistoryItem[] {
  return [...events]
    .sort((a, b) => b.height - a.height || b.tx_index - a.tx_index)
    .map((e, i) => ({
      key: `${e.height}-${e.tx_index}-${e.kind}-${i}`,
      text: eventText(e),
      date: formatDate(e.time),
      txHash: e.tx_hash,
    }));
}

/** "Never jailed, never penalised since <date>" when the events hold no jail or slash; null otherwise. */
export function cleanRecord(v: Pick<ValidatorDetail, "events" | "first_seen_time">): string | null {
  if (v.events.some((e) => e.kind === "jailed" || e.kind === "slashed" || e.kind === "tombstoned")) return null;
  return v.first_seen_time
    ? `Never jailed, never penalised since ${formatDate(v.first_seen_time)}`
    : "Never jailed, never penalised";
}

const OPTIONS: Record<string, string> = {
  yes: "Yes",
  no: "No",
  abstain: "Abstain",
  no_with_veto: "No with veto",
};

const TONES: Record<string, "green" | "red" | "gray" | "orange"> = { yes: "green", no: "red", abstain: "gray", no_with_veto: "orange" };

/** A vote's badge colour: green Yes, red No, gray Abstain, orange veto, purple split. */
export function voteTone(v: Pick<ValidatorVote, "option">): "green" | "red" | "gray" | "orange" | "purple" {
  return v.option ? TONES[v.option] ?? "gray" : "purple";
}

/** `Yes`, `No with veto`; `Split vote` for a weighted one. */
export function voteOption(v: Pick<ValidatorVote, "option">): string {
  return v.option ? OPTIONS[v.option] ?? humanise(v.option) : "Split vote";
}
