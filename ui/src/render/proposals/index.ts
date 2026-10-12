import type { ChainParams, Coin, Proposal, ProposalStatus, ProposalSummary, ProposalVote, Tally, TokenSummary, Validator, VoteOption } from "@/api/types";
import { formatAmount, formatDate, formatPct } from "@/lib/format";
import { compareDecimals, percentOf, sumAmounts } from "@/lib/math";

// The governance pages in plain words: kinds, statuses, the tally and the
// three checks a proposal must pass, dates and the timeline. Pure functions;
// the clock is always passed in.

export { describeProposal } from "./describe";
export type { Action, Entry } from "./actions";

const KIND_LABELS: Record<string, string> = {
  software_upgrade: "Software upgrade",
  community_pool_spend: "Community pool spend",
  parameter_change: "Parameter change",
  cointrunk_publisher: "CoinTrunk publisher",
  ibc_client_update: "Cross-chain repair",
  text: "Text",
  other: "Other",
};

/** The bond denom's display when the tokens list has not answered yet. */
const BZE = { symbol: "BZE", exponent: 6 };

/** `100,000 BZE and 2 USDC` with the tokens list's symbols and exponents; the raw amount and denom when unknown. */
export function coinsText(list: Coin[] | null | undefined, denoms: Map<string, Pick<TokenSummary, "symbol" | "exponent">> | undefined, compact = false): string {
  if (!list || list.length === 0) return "nothing";
  const parts = list.map((c) => {
    const t = denoms?.get(c.denom) ?? (c.denom === "ubze" ? BZE : undefined);
    return t?.symbol ? formatAmount(c.amount, t.exponent, t.symbol, { compact }) : `${c.amount} ${c.denom}`;
  });
  return parts.length === 1 ? parts[0] : `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;
}

export function kindLabel(kind: string): string {
  return KIND_LABELS[kind] ?? "Other";
}

export type Tone = "blue" | "green" | "red" | "gray" | "orange";

/**
 * The status pill. A proposal whose deposit period ended short of the minimum
 * deposit was dropped: the chain deleted it, the explorer keeps it. One still
 * in its deposit period after the period ended is shown dropped until the
 * drop is indexed.
 */
export function statusInfo(p: Pick<ProposalSummary, "status" | "deposit_end_time">, now: Date): { text: string; tone: Tone } {
  switch (p.status) {
    case "deposit_period":
      return p.deposit_end_time && new Date(p.deposit_end_time) < now
        ? { text: "Dropped", tone: "gray" }
        : { text: "Deposit period", tone: "orange" };
    case "voting_period":
      return { text: "Voting now", tone: "blue" };
    case "passed":
      return { text: "Passed", tone: "green" };
    case "rejected":
      return { text: "Rejected", tone: "red" };
    case "failed":
      return { text: "Failed", tone: "red" };
    case "canceled":
      return { text: "Canceled", tone: "gray" };
    case "dropped":
      return { text: "Dropped", tone: "gray" };
  }
}

/** The status in a page title: `Voting`, `Passed`. */
export function titleStatus(p: Pick<ProposalSummary, "status" | "deposit_end_time">, now: Date): string {
  return p.status === "voting_period" ? "Voting" : statusInfo(p, now).text;
}

export const isOpen = (status: ProposalStatus) => status === "deposit_period" || status === "voting_period";

export const OPTIONS: { option: VoteOption; label: string; tone: Tone }[] = [
  { option: "yes", label: "Yes", tone: "green" },
  { option: "no", label: "No", tone: "red" },
  { option: "abstain", label: "Abstain", tone: "gray" },
  { option: "no_with_veto", label: "Veto", tone: "orange" },
];

const OPTION_WORDS: Record<VoteOption, string> = { yes: "Yes", no: "No", abstain: "Abstain", no_with_veto: "No with veto" };

/** `Yes`, `No with veto`; `Split vote` for a weighted one. */
export function optionText(option: VoteOption | null): string {
  return option ? OPTION_WORDS[option] : "Split vote";
}

const tallyKey = (o: VoteOption): keyof Tally => o;

/** Each option's amount and share of the votes cast (percent; null before any vote). */
export function tallyShares(tally: Tally | null): { option: VoteOption; label: string; tone: Tone; amount: string; pct: string | null }[] {
  const t = tally ?? { yes: "0", no: "0", abstain: "0", no_with_veto: "0" };
  const total = sumAmounts([t.yes, t.no, t.abstain, t.no_with_veto]);
  return OPTIONS.map((o) => ({ ...o, amount: t[tallyKey(o.option)], pct: percentOf(t[tallyKey(o.option)], total) }));
}

/** The list's Result cell: the leading option and its share, `Yes 96.4%`; null before any vote. */
export function resultText(tally: Tally | null): string | null {
  const shares = tallyShares(tally).filter((s) => s.pct != null);
  if (shares.length === 0) return null;
  const top = shares.reduce((a, b) => (compareDecimals(b.amount, a.amount) > 0 ? b : a));
  return `${top.label} ${formatPct(top.pct!)}`;
}

/** The thresholds of the gov parameters, as fractions (`0.334`). */
export interface GovRules {
  quorum: string;
  threshold: string;
  vetoThreshold: string;
}

const fraction = (v: unknown) => (typeof v === "string" && v !== "" ? v : null);

/** The chain's own thresholds (the expedited ones for an expedited proposal); null when the parameters are unknown. */
export function govRules(params: ChainParams | undefined, expedited: boolean): GovRules | null {
  const gov = params?.gov?.params;
  if (!gov) return null;
  const quorum = fraction(expedited ? (gov.expedited_quorum ?? gov.quorum) : gov.quorum);
  const threshold = fraction(expedited ? gov.expedited_threshold : gov.threshold);
  const vetoThreshold = fraction(gov.veto_threshold);
  return quorum && threshold && vetoThreshold ? { quorum, threshold, vetoThreshold } : null;
}

export interface Check {
  ok: boolean;
  /** The measured share, percent. */
  value: string;
  /** The threshold, percent. */
  need: string;
}

export interface Checks {
  /** Turnout ≥ quorum. */
  turnout: Check;
  /** Yes ÷ (Yes + No + Veto) > threshold. */
  yes: Check;
  /** Veto ÷ all votes ≤ veto threshold. */
  veto: Check;
}

/** The three checks the chain runs when voting ends, on the current tally; null without a tally or the rules. */
export function checks(p: Pick<ProposalSummary, "tally" | "turnout_pct">, rules: GovRules | null): Checks | null {
  if (!p.tally || !rules) return null;
  const t = p.tally;
  const pct = (fractionValue: string) => percentOf(fractionValue, "1")!;
  const turnout = p.turnout_pct ?? "0";
  const yes = percentOf(t.yes, sumAmounts([t.yes, t.no, t.no_with_veto])) ?? "0";
  const veto = percentOf(t.no_with_veto, sumAmounts([t.yes, t.no, t.abstain, t.no_with_veto])) ?? "0";
  return {
    turnout: { ok: compareDecimals(turnout, pct(rules.quorum)) >= 0, value: turnout, need: pct(rules.quorum) },
    yes: { ok: compareDecimals(yes, pct(rules.threshold)) > 0, value: yes, need: pct(rules.threshold) },
    veto: { ok: compareDecimals(veto, pct(rules.vetoThreshold)) <= 0, value: veto, need: pct(rules.vetoThreshold) },
  };
}

/** `On track to pass` or `Would fail: <the first failing check>`. */
export function verdict(c: Checks): { pass: boolean; text: string } {
  if (!c.veto.ok) return { pass: false, text: "Would fail: vetoed" };
  if (!c.turnout.ok) return { pass: false, text: "Would fail: not enough people voted yet" };
  if (!c.yes.ok) return { pass: false, text: "Would fail: not enough Yes votes" };
  return { pass: true, text: "On track to pass" };
}

/** `in 3 days 4 h`, `in 5 h`, `in 12 min`; `ended` once past. */
export function endsIn(end: string, now: Date): string {
  const minutes = Math.floor((new Date(end).getTime() - now.getTime()) / 60_000);
  if (minutes <= 0) return "ended";
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor((minutes % 1440) / 60);
  if (days > 0) return `in ${days} ${days === 1 ? "day" : "days"}${hours > 0 ? ` ${hours} h` : ""}`;
  if (hours > 0) return `in ${hours} h`;
  return `in ${minutes} min`;
}

/** The list's Ended cell: the date, or `ends in 3 days` while voting; null before voting. */
export function endedText(p: Pick<ProposalSummary, "status" | "voting_end_time">, now: Date): string | null {
  if (!p.voting_end_time || p.status === "deposit_period" || p.status === "canceled" || p.status === "dropped") return null;
  if (p.status === "voting_period") {
    const days = Math.ceil((new Date(p.voting_end_time).getTime() - now.getTime()) / 86_400_000);
    return days <= 1 ? "ends today" : `ends in ${days} days`;
  }
  return formatDate(p.voting_end_time);
}

/** The header's result line: `Passed Oct 3, 2026 · 100% Yes · 70.4% turnout`. */
export function resultLine(p: Pick<ProposalSummary, "status" | "voting_end_time" | "tally" | "turnout_pct" | "deposit_end_time">, now: Date): string {
  const parts = [`${statusInfo(p, now).text}${p.voting_end_time ? ` ${formatDate(p.voting_end_time)}` : ""}`];
  const yes = tallyShares(p.tally)[0];
  if (p.tally && yes.pct != null) parts.push(`${formatPct(yes.pct, { trim: true })} Yes`);
  if (p.turnout_pct != null) parts.push(`${formatPct(p.turnout_pct)} turnout`);
  return parts.join(" · ");
}

/** When the chain reaches `height`: now + blocks left × the average block time; null once reached or unknown. */
export function upgradeEta(height: number, dbHeight: number | null | undefined, avgBlockMs: number | null | undefined, now: Date): Date | null {
  if (dbHeight == null || avgBlockMs == null || avgBlockMs <= 0 || height <= dbHeight) return null;
  return new Date(now.getTime() + (height - dbHeight) * avgBlockMs);
}

/** The bonded validators whose owner has not voted, largest first. */
export function notVoted<V extends Pick<Validator, "operator_address" | "status" | "tokens">>(validators: V[], votes: Pick<ProposalVote, "validator_operator">[]): V[] {
  const voted = new Set(votes.map((v) => v.validator_operator));
  return validators
    .filter((v) => v.status === "bonded" && !voted.has(v.operator_address))
    .sort((a, b) => compareDecimals(b.tokens, a.tokens));
}

/** `19 validators voted Yes, 1 voted No, 2 did not vote. 842 accounts voted on their own.` */
export function votesSummary(validatorVotes: Pick<ProposalVote, "option">[], notVotedCount: number, votesCount: number): string {
  const by = new Map<string, number>();
  for (const v of validatorVotes) {
    const key = optionText(v.option);
    by.set(key, (by.get(key) ?? 0) + 1);
  }
  const parts = [...by.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([option, n], i) => `${n} ${i === 0 ? (n === 1 ? "validator voted" : "validators voted") : "voted"} ${option}`);
  if (notVotedCount > 0) parts.push(`${notVotedCount} did not vote`);
  const own = Math.max(0, votesCount - validatorVotes.length);
  const first = parts.length > 0 ? `${parts.join(", ")}.` : "No validator voted.";
  return `${first} ${own === 1 ? "1 account" : `${own} accounts`} voted on ${own === 1 ? "its" : "their"} own.`;
}

/** The first `max` characters of the summary, cut at a word, with an ellipsis. */
export function excerpt(summary: string | null, max: number): string {
  const s = (summary ?? "").replace(/\s+/g, " ").trim();
  if (s.length <= max) return s;
  const cut = s.slice(0, max);
  const space = cut.lastIndexOf(" ");
  return `${(space > max / 2 ? cut.slice(0, space) : cut).replace(/[\s.,;:]+$/, "")}…`;
}

/** The proposal's `metadata`: a link when it is a URL, text otherwise; null when empty. */
export function metadataView(metadata: string | null): { href: string } | { text: string } | null {
  const m = metadata?.trim();
  if (!m) return null;
  if (/^https?:\/\/\S+$/i.test(m)) return { href: m };
  if (/^ipfs:\/\/\S+$/i.test(m)) return { href: `https://ipfs.io/ipfs/${m.slice("ipfs://".length)}` };
  return { text: m };
}

/** Whether the deposits went back to the depositors: passed or failed, or rejected without a veto. */
export function depositsReturned(status: ProposalStatus, vetoed: boolean): boolean {
  return status === "passed" || status === "failed" || (status === "rejected" && !vetoed);
}

export interface TimelineItem {
  label: string;
  time: string | null;
  /** A block to link. */
  height: number | null;
  /** Before `now`. */
  done: boolean;
}

/** Submitted, deposit reached and voting opened, voting ends, the result, and an upgrade's block. */
export function timeline(
  p: Pick<Proposal, "status" | "submit_time" | "submit_height" | "deposit_end_time" | "voting_start_time" | "voting_end_time" | "resolved_height">,
  depositText: string | null,
  upgrade: { height: number; eta: Date | null; reached: boolean } | null,
  now: Date,
): TimelineItem[] {
  const past = (t: string | null) => t != null && new Date(t) <= now;
  const items: TimelineItem[] = [{ label: "Submitted", time: p.submit_time, height: p.submit_height, done: true }];
  if (p.voting_start_time) {
    items.push({
      label: depositText ? `Deposit of ${depositText} reached, voting opened` : "Deposit reached, voting opened",
      time: p.voting_start_time,
      height: null,
      done: past(p.voting_start_time),
    });
  } else if (p.deposit_end_time) {
    items.push({
      label: past(p.deposit_end_time) ? "Deposit not reached, dropped" : "Deposit period ends",
      time: p.deposit_end_time,
      height: null,
      done: past(p.deposit_end_time),
    });
  }
  if (p.voting_end_time && p.voting_start_time) {
    items.push({ label: past(p.voting_end_time) ? "Voting ended" : "Voting ends", time: p.voting_end_time, height: null, done: past(p.voting_end_time) });
  }
  const result: Partial<Record<ProposalStatus, string>> = {
    passed: "Passed and executed",
    rejected: "Rejected",
    failed: "Passed, but its execution failed",
  };
  if (result[p.status]) {
    items.push({ label: result[p.status]!, time: p.voting_end_time, height: p.resolved_height, done: true });
  }
  if (upgrade && (p.status === "passed" || p.status === "voting_period")) {
    const eta = upgrade.eta?.toISOString() ?? null;
    items.push({
      label: upgrade.reached ? "Upgrade happened" : upgrade.eta ? "Upgrade happens" : "Upgrade block",
      time: eta,
      height: upgrade.height,
      done: upgrade.reached,
    });
  }
  return items;
}
