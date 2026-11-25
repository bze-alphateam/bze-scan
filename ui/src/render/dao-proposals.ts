import BigNumber from "bignumber.js";

import type { DaoProposal, DaoProposalStatus, DaoProposalSummary, DaoVote, DaoWeightDenom } from "@/api/types";
import { formatAmount, formatPct } from "@/lib/format";

import { ratioText } from "./daos";

/** The DAO proposal's page. */
export const daoProposalHref = (daoId: number | string, id: number | string) => `/daos/${daoId}/proposals/${id}`;

/** The summary a proposer wrote is shown up to this many characters. */
export const SUMMARY_LIMIT = 5000;

export type StatusTone = "accent" | "ok" | "bad" | "warn" | "muted";

export interface StatusInfo {
  /** The tag and chip text. */
  label: string;
  tone: StatusTone;
}

const STATUSES: Record<DaoProposalStatus, StatusInfo> = {
  voting: { label: "Voting", tone: "accent" },
  executed: { label: "Passed (executed)", tone: "ok" },
  execution_failed: { label: "Failed to execute", tone: "warn" },
  rejected: { label: "Rejected", tone: "bad" },
  expired: { label: "Expired", tone: "muted" },
  cancelled: { label: "Cancelled", tone: "muted" },
};

/** The chips over the list, in the order the ticket names them; `null` is every status. */
export const STATUS_FILTERS: { label: string; value: DaoProposalStatus | null }[] = [
  { label: "All", value: null },
  { label: "Voting", value: "voting" },
  { label: "Passed", value: "executed" },
  { label: "Failed to execute", value: "execution_failed" },
  { label: "Rejected", value: "rejected" },
  { label: "Expired", value: "expired" },
  { label: "Cancelled", value: "cancelled" },
];

export function statusInfo(status: string | null): StatusInfo {
  return (status && STATUSES[status as DaoProposalStatus]) || { label: "Not open", tone: "muted" };
}

const big = (v: string | null | undefined) => new BigNumber(v ?? "");

/** `part` as a percentage of `whole` (a string, `"12.3456"`); null when either is unknown or the whole is zero. */
export function percentOf(part: string | null | undefined, whole: string | null | undefined): string | null {
  const w = big(whole);
  const p = big(part);
  if (!w.isFinite() || !p.isFinite() || w.lte(0)) return null;
  return p.times(100).dividedBy(w).toString();
}

export interface Shares {
  yes: string | null;
  no: string | null;
  abstain: string | null;
  /** Everyone who voted: yes + no + abstain. */
  turnout: string | null;
}

/** Each option's, and the turnout's, share of the voting power pinned at submission. */
export function shares(p: Pick<DaoProposalSummary, "yes" | "no" | "abstain" | "denominator">): Shares {
  const cast = big(p.yes).plus(big(p.no)).plus(big(p.abstain));
  return {
    yes: percentOf(p.yes, p.denominator),
    no: percentOf(p.no, p.denominator),
    abstain: percentOf(p.abstain, p.denominator),
    turnout: cast.isFinite() ? percentOf(cast.toFixed(0), p.denominator) : null,
  };
}

/** Yes ÷ (yes + no), in percent: what the threshold is measured against. Null with no yes or no votes. */
export function yesOfCast(p: Pick<DaoProposalSummary, "yes" | "no">): string | null {
  const yes = big(p.yes);
  const no = big(p.no);
  if (!yes.isFinite() || !no.isFinite()) return null;
  return percentOf(p.yes, yes.plus(no).toFixed(0));
}

/** A bar of a pinned config as the text after "needed": `40%`. Null when the config lacks it. */
function bar(config: Record<string, unknown> | null, key: string): string | null {
  const v = config?.[key];
  return typeof v === "string" && big(v).isFinite() ? ratioText(v) : null;
}

export interface CheckLine {
  key: string;
  title: string;
  /** Null when the API had no answer. */
  ok: boolean | null;
  detail: string;
}

export interface CheckGroup {
  key: "regular" | "expedite";
  title: string;
  note: string;
  lines: CheckLine[];
}

const pct = (v: string | null) => (v == null ? "—" : formatPct(v));
const needed = (v: string | null) => (v == null ? "" : ` · ${v} needed`);

/**
 * The result as checks, from the API's verdicts (`checks`), with the figures
 * that explain them. The regular rule: turnout at or above the quorum and Yes
 * strictly above the threshold of Yes + No (a tie fails). An expedited
 * proposal adds the early-settle bars, both against the full voting power.
 */
export function checkGroups(p: DaoProposal): CheckGroup[] {
  if (!p.checks) return [];
  const s = shares(p);
  const cfg = p.pinned_config;
  const groups: CheckGroup[] = [
    {
      key: "regular",
      title: "When voting ends",
      note: "Anyone can settle the proposal once the voting period is over.",
      lines: [
        {
          key: "quorum",
          title: "Enough turnout",
          ok: p.checks.regular.quorum_met,
          detail: `${pct(s.turnout)} of the voting power voted${needed(bar(cfg, "min_quorum"))}`,
        },
        {
          key: "threshold",
          title: "Yes above the threshold",
          ok: p.checks.regular.threshold_met,
          detail: `${pct(yesOfCast(p))} of the Yes and No votes are Yes · more than ${bar(cfg, "threshold") ?? "the threshold"} needed, a tie fails`,
        },
      ],
    },
  ];
  if (p.checks.expedite) {
    groups.push({
      key: "expedite",
      title: "To settle early",
      note: "An expedited proposal can be settled before the voting period ends once both bars are met.",
      lines: [
        {
          key: "expedite-quorum",
          title: "Enough turnout for an early finish",
          ok: p.checks.expedite.quorum_met,
          detail: `${pct(s.turnout)} of the full voting power voted${needed(bar(cfg, "expedite_quorum"))}`,
        },
        {
          key: "expedite-yes",
          title: "Enough Yes votes for an early finish",
          ok: p.checks.expedite.yes_met,
          detail: `${pct(s.yes)} of the full voting power said Yes · more than ${bar(cfg, "expedite_threshold") ?? "the threshold"} needed`,
        },
      ],
    });
  }
  return groups;
}

/** What the result card says above the checks while the proposal is open for settling; null otherwise. */
export function settleNote(p: Pick<DaoProposal, "status" | "voting_ended" | "expedite_settleable">): string | null {
  if (p.status !== "voting") return null;
  if (p.voting_ended) return "Voting ended; anyone can settle it now";
  if (p.expedite_settleable) return "Can be settled early";
  return null;
}

/** A voting-power amount in words: stake DAOs in display units of the weight denom, member lists as votes. */
export function weightText(amount: string | null | undefined, wd: DaoWeightDenom | null | undefined, compact = false): string {
  if (amount == null) return "—";
  if (!wd) {
    // A member list counts one vote per member; a stake DAO whose denom is unknown shows the raw weight.
    return `${amount} ${amount === "1" ? "vote" : "votes"}`;
  }
  return wd.symbol != null && wd.exponent != null ? formatAmount(amount, wd.exponent, wd.symbol, { compact }) : `${amount} ${wd.denom}`;
}

/** Whether part of a vote's weight is gone: its effective weight is below the weight it was cast with. */
export function isReduced(v: Pick<DaoVote, "weight" | "effective_weight">): boolean {
  const w = big(v.weight);
  const e = big(v.effective_weight);
  return w.isFinite() && e.isFinite() && e.lt(w);
}

export const REDUCED_NOTE_STAKE = "part of this stake left before the vote ended";
export const REDUCED_NOTE_MEMBER = "this member left the DAO before the vote ended";

/** The summary a proposer wrote, cut to the limit; `cut` says it was. */
export function summaryText(summary: string | null): { text: string; cut: boolean } | null {
  const s = summary?.trim();
  if (!s) return null;
  const chars = Array.from(s);
  return chars.length > SUMMARY_LIMIT ? { text: chars.slice(0, SUMMARY_LIMIT).join(""), cut: true } : { text: s, cut: false };
}

/** The type URL of a proto-JSON message. */
export const msgType = (msg: Record<string, unknown>): string => (typeof msg["@type"] === "string" ? msg["@type"] : "");

/** A proposal's body is shown only as far as the API gave it: the ones it could not read come as null. */
export const hasMessages = (p: Pick<DaoProposal, "msgs">): boolean => Array.isArray(p.msgs) && p.msgs.length > 0;
