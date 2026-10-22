import BigNumber from "bignumber.js";

import type {
  ChainParams,
  HallOfFameRules,
  StakeChange,
  StakeChanges,
  StakeMovement,
  Validator,
  ValidatorDetail,
  ValidatorEvent,
  ValidatorVote,
} from "@/api/types";
import { formatAmount, formatCount, formatDate, formatDateTime, formatPct, shortAddress } from "@/lib/format";
import { aboutDuration } from "@/lib/parameters";

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

/** The Hall of Fame rule in the words the page uses: the proposals voted on, the most blocks missed of the window. */
export interface FameRule {
  proposals: number;
  maxMissed: number;
  window: number;
}

/**
 * The crown card's signing line, with the rule's allowance: "Missed 4 of the
 * last 10,000 blocks (10 allowed)". The window is the rule's, else the validator's.
 */
export function fameMissedText(v: Pick<Validator, "missed_blocks" | "signed_blocks_window">, rules: HallOfFameRules): string {
  const allowed = `(${formatCount(rules.max_missed_blocks)} allowed)`;
  const window = rules.signed_blocks_window ?? v.signed_blocks_window;
  const last = window != null ? ` the last ${formatCount(window)}` : "";
  if (v.missed_blocks == null) return `Signing record not known yet ${allowed}`;
  if (v.missed_blocks === 0) return `Missed no blocks in${last || " the window"} ${allowed}`;
  return `Missed ${formatCount(v.missed_blocks)} of${last || " the window's"} blocks ${allowed}`;
}

/** An uptime's tone in the lists: red under 99.5%, muted at a full 100%, plain between. */
export function uptimeTone(uptime: string | null | undefined): "bad" | "muted" | null {
  if (uptime == null) return null;
  const n = Number(uptime);
  if (!Number.isFinite(n)) return null;
  if (n < 99.5) return "bad";
  return n >= 100 ? "muted" : null;
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

/** An active validator's signing right now; null outside the active set or while unknown. */
export type OnlineState = "online" | "offline";

export function onlineState(v: Pick<Validator, "offline">): OnlineState | null {
  if (v.offline == null) return null;
  return v.offline ? "offline" : "online";
}

/** "about 11 hours" for `blocks` blocks at the average block time; null without one. */
function blocksDuration(blocks: number, avgBlockMs: number | null | undefined): string | null {
  if (!avgBlockMs || avgBlockMs <= 0) return null;
  return aboutDuration((blocks * avgBlockMs) / 1000);
}

/** The lines of the online / offline card: what the dot means for this validator. */
export function onlineLines(
  v: Pick<Validator, "offline" | "missed_in_a_row" | "last_signed_height">,
  avgBlockMs?: number | null,
): string[] {
  const missed = v.missed_in_a_row ?? 0;
  const last = v.last_signed_height;
  if (v.offline) {
    const span = blocksDuration(missed, avgBlockMs);
    return [
      `Missed the last ${formatCount(missed)} blocks in a row${span ? `, ${span}` : ""}.`,
      last != null ? `Last signed block ${formatCount(last)}.` : "Not seen signing since the explorer started watching.",
    ];
  }
  if (last == null) return ["Signing blocks."];
  if (missed === 0) return [`Signed the latest block, ${formatCount(last)}.`];
  return [`Signed block ${formatCount(last)}, ${formatCount(missed)} ${missed === 1 ? "block" : "blocks"} ago.`];
}

/**
 * How long an offline validator has before the slashing module jails it, if every next block is missed too:
 * "about 2 hours"; "any moment now" when the limit is reached; null when the parameters or the block time are not known.
 */
export function jailedInText(
  v: Pick<Validator, "missed_blocks" | "signed_blocks_window">,
  params: ChainParams | undefined,
  avgBlockMs: number | null | undefined,
): string | null {
  const window = v.signed_blocks_window ?? signedBlocksWindow(params);
  const minSigned = new BigNumber(String(params?.slashing?.params?.min_signed_per_window ?? "NaN"));
  if (window == null || v.missed_blocks == null || !minSigned.isFinite()) return null;
  // x/slashing: jailed once missed > window − trunc(window × min_signed_per_window).
  const maxMissed = window - minSigned.times(window).integerValue(BigNumber.ROUND_DOWN).toNumber();
  const left = maxMissed - v.missed_blocks + 1;
  if (left <= 1) return "any moment now";
  return blocksDuration(left, avgBlockMs);
}

/** The downtime penalty as a percentage of the stake: "0.01%"; null when unknown. */
export function downtimeSlashText(params: ChainParams | undefined): string | null {
  const raw = params?.slashing?.params?.slash_fraction_downtime;
  const f = new BigNumber(String(raw ?? "NaN"));
  // Small fractions keep their digits (0.0001 is 0.01%, not 0%).
  return f.isFinite() ? `${f.times(100).decimalPlaces(4).toFixed()}%` : null;
}

// Stake changes and movements

const MINUS = "\u2212";

/**
 * A signed BZE amount from signed ubze, compact and always with its sign:
 * `+122.0K`, `\u22121.64M`, `+250`, `+0.5`. Millions and up keep two decimals,
 * thousands one, smaller amounts at most two (rounded half up).
 */
export function signedBondCompact(ubze: string): string {
  const units = new BigNumber(ubze).shiftedBy(-BOND.exponent);
  if (!units.isFinite()) return ubze;
  const sign = units.isNegative() ? MINUS : "+";
  const abs = units.abs();
  const scaled: [number, string, number][] = [
    [1e9, "B", 2],
    [1e6, "M", 2],
    [1e3, "K", 1],
  ];
  const unit = scaled.find(([size]) => abs.gte(size));
  if (unit) {
    const [size, suffix, decimals] = unit;
    return `${sign}${abs.dividedBy(size).toFixed(decimals, BigNumber.ROUND_HALF_UP)}${suffix}`;
  }
  const small = abs.decimalPlaces(2, BigNumber.ROUND_HALF_UP);
  return `${sign}${small.isZero() ? "<0.01" : small.toFormat()}`;
}

/** One window of a stake change as shown: `24h ▲ +122.0K`. */
export interface StakeChangeLine {
  window: "24h" | "7d";
  /** Growth (`ok` tone, ▲) or shrink (`warn` tone, ▼). */
  up: boolean;
  text: string;
}

/**
 * The windows of a validator's change worth showing: none while the window is
 * not fully indexed (or not loaded), none for a validator without movements,
 * and never a window whose net is 0.
 */
export function stakeChangeLines(changes: StakeChanges | undefined, operator: string): StakeChangeLine[] {
  if (!changes?.complete) return [];
  const change: StakeChange | undefined = changes.items[operator];
  if (!change) return [];
  return (["24h", "7d"] as const).flatMap((window) => {
    const net = new BigNumber(change[window]);
    if (!net.isFinite() || net.isZero()) return [];
    return [{ window, up: net.gt(0), text: signedBondCompact(change[window]) }];
  });
}

/** True for the movements that add stake to the validator. */
export function movementAdds(kind: StakeMovement["kind"]): boolean {
  return kind !== "undelegate" && kind !== "redelegate_out";
}

/** A movement's amount, signed by its direction: `+640,000 BZE`, `\u22125 BZE`. */
export function movementAmount(m: Pick<StakeMovement, "kind" | "amount">): string {
  return `${movementAdds(m.kind) ? "+" : MINUS}${bondFull(m.amount)}`;
}

/**
 * A movement in words: `Delegated`, or for a redelegation the words before the
 * other validator (`Moved to`) and that validator's name (its moniker, else
 * its shortened address).
 */
export function movementLabel(m: Pick<StakeMovement, "kind" | "counterparty_validator">): {
  text: string;
  counterparty: { operator: string; name: string } | null;
} {
  const cp = m.counterparty_validator;
  const counterparty = cp ? { operator: cp.operator_address, name: cp.moniker ?? shortAddress(cp.operator_address) } : null;
  switch (m.kind) {
    case "delegate":
      return { text: "Delegated", counterparty: null };
    case "undelegate":
      return { text: "Undelegated", counterparty: null };
    case "redelegate_out":
      return { text: counterparty ? "Moved to" : "Moved to another validator", counterparty };
    case "redelegate_in":
      return { text: counterparty ? "Moved from" : "Moved from another validator", counterparty };
    case "cancel_unbonding":
      return { text: "Cancelled unbonding", counterparty: null };
    case "create":
      return { text: "Initial self-stake", counterparty: null };
    default:
      return { text: String(m.kind), counterparty: null };
  }
}

/** "Movements 11–20 of 214"; "Movements 0 of 0" when there is none. */
export function movementsRange(page: number, perPage: number, total: number): string {
  const from = (page - 1) * perPage + 1;
  if (total === 0 || from > total) return `Movements 0 of ${formatCount(total)}`;
  return `Movements ${formatCount(from)}–${formatCount(Math.min(total, page * perPage))} of ${formatCount(total)}`;
}
