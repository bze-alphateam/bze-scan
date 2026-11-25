import type { Emergency, EmergencyAction, EmergencyChannel, EmergencyDenom } from "@/api/types";
import { formatCount } from "@/lib/format";
import { humanise } from "@/render/messages/common";
import type { RenderMessage } from "@/render/messages/types";

import { durationText } from "./future-block";

// The emergency page and the site-wide banner in plain words. Pure functions
// of the `/emergency` answer, so both read the same.

const plural = (n: number, one: string, many: string) => `${formatCount(n)} ${n === 1 ? one : many}`;

/** A token as the page names it: its ticker, else the last piece of its denom. */
export function emergencyTokenName(d: Pick<EmergencyDenom, "denom" | "symbol">): string {
  return d.symbol ?? d.denom.split("/").pop() ?? d.denom;
}

/** "channel-0 to Osmosis"; the other chain only when the explorer knows it. */
export function channelText(c: EmergencyChannel): string {
  const other = c.counterparty_chain_name ?? c.counterparty_chain_id;
  return other ? `${c.channel_id} to ${other}` : c.channel_id;
}

/** Whether an emergency chain halt is scheduled. */
export const hasScheduledHalt = (e: Pick<Emergency, "plan">) => e.plan?.emergency === true;

/** How many switches are in force: each list with items, paused automation, a scheduled emergency halt. */
export function measureCount(e: Emergency): number {
  return (
    [e.halted_denoms.length > 0, e.blocked_denoms.length > 0, e.closed_channels.length > 0, e.disabled_messages.length > 0, e.automation_paused, hasScheduledHalt(e)].filter(Boolean)
      .length
  );
}

/**
 * The parts of the banner line, in the order of the page. `chainPaused`
 * drops the halt part: the pause strip already carries it.
 */
export function bannerParts(e: Emergency, chainPaused = false): string[] {
  const parts: string[] = [];
  if (e.halted_denoms.length) parts.push(`trading halted for ${plural(e.halted_denoms.length, "token", "tokens")}`);
  if (e.blocked_denoms.length) parts.push(`${plural(e.blocked_denoms.length, "token", "tokens")} blocked from crossing chains`);
  if (e.closed_channels.length) parts.push(`${plural(e.closed_channels.length, "channel", "channels")} closed`);
  if (e.disabled_messages.length) parts.push(`${plural(e.disabled_messages.length, "kind", "kinds")} of actions switched off`);
  if (e.automation_paused) parts.push("fee conversion and burning paused");
  if (hasScheduledHalt(e) && !chainPaused) parts.push(`chain halt scheduled at block ${formatCount(e.plan!.height)}`);
  return parts;
}

/** The banner's text: at most 3 parts, then "+ N more". */
export function bannerText(parts: string[], max = 3): string {
  const shown = parts.slice(0, max).join(" · ");
  const more = parts.length - max;
  return more > 0 ? `${shown} + ${more} more` : shown;
}

/** The time left to a block, from the average block time; null when it cannot be told. */
export function haltCountdown(e: Pick<Emergency, "height" | "plan">, avgBlockMs: number | null): string | null {
  if (!e.plan || !avgBlockMs || avgBlockMs <= 0) return null;
  const blocks = e.plan.height - e.height;
  if (blocks <= 0) return null;
  return `in about ${durationText(blocks * avgBlockMs)} (${formatCount(blocks)} blocks)`;
}

/** The DAO the guardian is, from a label like "DAO #3 · Ops"; null for any other. */
export function guardianDao(label: string | null | undefined): { id: number; name: string | null } | null {
  const m = label?.match(/^DAO #(\d+)(?:\s*·\s*(.+))?$/);
  return m ? { id: Number(m[1]), name: m[2] ?? null } : null;
}

/** The token a halted or IBC-blocked token page warns about; null for a token no switch touches. */
export function tokenSwitches(e: Emergency | undefined, denom: string): { halted: boolean; ibcBlocked: boolean } {
  return {
    halted: e?.halted_denoms.some((d) => d.denom === denom) ?? false,
    ibcBlocked: e?.blocked_denoms.some((d) => d.denom === denom) ?? false,
  };
}

// History rows

const GUARDIAN = "/bze.guardian.v1.";

/** The message type a history row is told with by the guardian renderers. */
const ACTION_TYPE: Record<string, string> = {
  tighten_rate_limit: `${GUARDIAN}MsgTightenRateLimit`,
  block_denom: `${GUARDIAN}MsgBlockDenom`,
  unblock_denom: `${GUARDIAN}MsgUnblockDenom`,
  disable_messages: `${GUARDIAN}MsgDisableMessages`,
  halt_trading: `${GUARDIAN}MsgHaltTradingDenoms`,
  halt_chain: `${GUARDIAN}MsgHaltChain`,
  pause_automation: `${GUARDIAN}MsgPauseAutomation`,
  resume_automation: `${GUARDIAN}MsgResumeAutomation`,
  close_channel: `${GUARDIAN}MsgCloseChannel`,
  reopen_channel: `${GUARDIAN}MsgReopenChannel`,
  set_guardian: `${GUARDIAN}MsgUpdateParams`,
};

/** The event a block-level row's attributes belong to, so the renderers can read them as they would a transaction's. */
const ACTION_EVENT: Record<string, string> = {
  tighten_rate_limit: "TightenRateLimitEvent",
  halt_chain: "ChainHaltScheduledEvent",
};

/** Titles of the undos the guardian renderers do not cover (governance runs them). */
const OWN_TITLE: Record<string, (a: EmergencyAction) => string> = {
  unhalt_trading: (a) => {
    const n = Array.isArray(a.details.denoms) ? a.details.denoms.length : 0;
    return n ? `Lifted the trading halt of ${plural(n, "token", "tokens")}` : "Lifted a trading halt";
  },
  enable_messages: (a) => {
    const urls = Array.isArray(a.details.msg_type_urls) ? a.details.msg_type_urls.map(String) : [];
    return urls.length ? `Switched back on ${urls.map(humanise).join(", ")}` : "Switched actions back on";
  },
};

/** The history row as a message the renderers read; null for an action that has its own title. */
export function actionMessage(a: EmergencyAction): RenderMessage | null {
  const type_url = ACTION_TYPE[a.action];
  if (!type_url) return null;
  const event = ACTION_EVENT[a.action];
  const blockLevel = a.tx_hash === null && a.proposal_id === null;
  const body: Record<string, unknown> = { ...a.details };
  if (a.by === "guardian" && a.signer) body.signer = a.signer;
  if (a.by === "governance" && a.signer && body.authority === undefined) body.authority = a.signer;
  return {
    type_url,
    sender: a.signer,
    module: "guardian",
    body,
    events: blockLevel && event ? [{ type: `bze.guardian.v1.${event}`, attrs: a.details }] : null,
    msg_index: null,
  };
}

/** The row's own title when no guardian renderer covers it. */
export function ownTitle(a: EmergencyAction): string {
  return OWN_TITLE[a.action]?.(a) ?? humanise(a.action.replace(/_/g, " "));
}

/** Where a history row leads: its proposal, its transaction, or its block. */
export function actionHref(a: EmergencyAction): string {
  if (a.proposal_id !== null) return `/proposals/${a.proposal_id}`;
  if (a.tx_hash) return `/txs/${encodeURIComponent(a.tx_hash.toUpperCase())}`;
  return `/blocks/${a.height}`;
}

export function actionLinkText(a: EmergencyAction): string {
  if (a.proposal_id !== null) return `Proposal #${a.proposal_id}`;
  if (a.tx_hash) return "Transaction";
  return `Block ${formatCount(a.height)}`;
}

export const IBC_PAGE = "/ibc";
