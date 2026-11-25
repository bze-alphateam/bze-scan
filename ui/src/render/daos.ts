import BigNumber from "bignumber.js";

import type { Dao, DaoEvent, DaoSummary, Label } from "@/api/types";
import { formatPct, shortAddress } from "@/lib/format";

/** The DAO's page. */
export const daoHref = (id: number | string) => `/daos/${id}`;

/** `3` from `"3"` or `"DAO #3 · Name"`-like text: a positive integer id, else null (the route's rule). */
export function parseDaoId(raw: string): number | null {
  if (!/^[1-9]\d{0,15}$/.test(raw)) return null;
  const id = Number(raw);
  return Number.isSafeInteger(id) ? id : null;
}

export function typeLabel(type: string): string {
  return type === "stake" ? "Stakers" : "Member list";
}

/** One line on who can vote, for a list row or a tag's tooltip. */
export function typeExplainer(type: string): string {
  return type === "stake"
    ? "Voting power is stake in the DAO's reward programs, counted only if it joined before the proposal."
    : "A fixed list of members, one vote each.";
}

/** The DAO an address is, from a `dao` label "DAO #7 · Name"; null for any other label. */
export function daoOfLabel(label: Label | null | undefined): { id: number; name: string } | null {
  if (label?.kind !== "dao") return null;
  const m = /^DAO #(\d+)(?: · (.*))?$/s.exec(label.name);
  return m ? { id: Number(m[1]), name: m[2] ?? "" } : null;
}

/** `Self-governed`, `Run by <who>`: the admin's chip is drawn by the caller, this is the words around it. */
export type AdminState = "self" | "admin";

export function adminState(d: Pick<DaoSummary, "self_governed">): AdminState {
  return d.self_governed ? "self" : "admin";
}

/** A ratio such as `0.334000000000000000` as a percentage: `33.4%`. */
export function ratioText(ratio: string): string {
  const v = new BigNumber(ratio);
  return v.isFinite() ? formatPct(v.times(100).toString(), { trim: true }) : ratio;
}

/** `3 days`, `12 hours`, `90 minutes`, `1 day 12 h`: a duration in seconds in the largest whole units. */
export function durationText(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "none";
  const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? "" : "s"}`;
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  if (days > 0) return hours > 0 ? `${plural(days, "day")} ${hours} h` : plural(days, "day");
  const minutes = Math.floor((seconds % 3600) / 60);
  if (hours > 0) return minutes > 0 ? `${plural(hours, "hour")} ${minutes} min` : plural(hours, "hour");
  return plural(Math.max(1, minutes), "minute");
}

/** The tail of a proto enum name: `DEPOSIT_REFUND_POLICY_ONLY_PASSED` and `ONLY_PASSED` both end in `ONLY_PASSED`. */
function endsWith(value: string, name: string): boolean {
  return value.toUpperCase().replace(/-/g, "_").endsWith(name);
}

export function refundText(policy: string): string {
  if (endsWith(policy, "ONLY_PASSED")) return "Refunded only if the proposal passes";
  if (endsWith(policy, "ALWAYS")) return "Always refunded";
  if (endsWith(policy, "NEVER")) return "Never refunded";
  return policy;
}

export function destinationText(destination: string): string {
  if (endsWith(destination, "BURN")) return "burned";
  if (endsWith(destination, "TREASURY")) return "kept in the DAO's treasury";
  return destination;
}

/** The staking-reward and denom-reward programs a stake DAO counts, as short words. */
export function sourceKind(source: { sr_id: string | null; dr_denom: string | null }): string {
  return source.sr_id != null ? `Staking reward ${source.sr_id}` : `Denom reward of ${source.dr_denom ?? ""}`;
}

/** A list the chain wrote into an event: an array, a JSON array in a string or a comma-separated string. */
export function listAttr(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String);
  if (typeof value !== "string" || value === "") return [];
  if (value.startsWith("[")) {
    try {
      const parsed: unknown = JSON.parse(value);
      if (Array.isArray(parsed)) return parsed.map(String);
    } catch {
      // Not JSON: fall through to the comma form.
    }
  }
  return value.split(",").map((s) => s.trim()).filter(Boolean);
}

const attr = (e: DaoEvent, key: string): string => {
  const v = e.attrs?.[key];
  return typeof v === "string" || typeof v === "number" ? String(v) : "";
};

/** What a config field is called in plain words (the chain's proto names). */
const FIELD_WORDS: Record<string, string> = {
  min_quorum: "quorum",
  threshold: "threshold",
  voting_period: "voting period",
  allow_expedite: "expedited voting",
  expedited_period: "expedited period",
  expedite_quorum: "expedited quorum",
  expedite_threshold: "expedited threshold",
  open_submission: "who can propose",
  proposal_cost: "proposal cost",
  proposal_deposit: "proposal deposit",
  deposit_refund_policy: "deposit refund",
  cost_destination: "cost destination",
  kept_deposit_destination: "kept deposit destination",
};

export function fieldWords(fields: string[]): string {
  return fields.map((f) => FIELD_WORDS[f] ?? f.replace(/_/g, " ")).join(", ");
}

/** One history line, split so the page can draw the addresses as chips: text parts and address parts, in order. */
export type HistoryPart = { text: string } | { address: string };

const t = (text: string): HistoryPart => ({ text });
const a = (address: string): HistoryPart => ({ address });

/** The plain-words line of one DAO change, newest first in the tab. `creator` fills "Created by …" when the row has none. */
export function historyLine(e: DaoEvent, dao: Pick<Dao, "creator">): HistoryPart[] {
  switch (e.type) {
    case "EventDaoCreated": {
      const by = dao.creator;
      return by ? [t("Created by "), a(by)] : [t("Created")];
    }
    case "EventAdminChanged": {
      const from = attr(e, "old_admin");
      const to = attr(e, "new_admin");
      return [t("Admin changed from "), ...(from ? [a(from)] : [t("—")]), t(" to "), ...(to ? [a(to)] : [t("—")])];
    }
    case "EventAdminNominated":
      return [t("Admin handover offered to "), a(attr(e, "nominee"))];
    case "EventAdminNominationWithdrawn":
      return [t("Admin handover to "), a(attr(e, "nominee")), t(" withdrawn")];
    case "EventAdminExecuted": {
      const n = Number(attr(e, "msg_count")) || 0;
      return [t(`Admin ran ${n} ${n === 1 ? "message" : "messages"} as the DAO`)];
    }
    case "EventDaoConfigUpdated": {
      const fields = listAttr(e.attrs?.fields);
      return [t(fields.length > 0 ? `Rules changed: ${fieldWords(fields)}` : "Rules changed")];
    }
    case "EventDaoMetadataUpdated":
      return [t("Name and description changed")];
    case "EventDaoElectorateUpdated": {
      const sr = listAttr(e.attrs?.sr_ids);
      const dr = attr(e, "dr_denom");
      const programs = [...(sr.length > 0 ? [`staking rewards ${sr.join(", ")}`] : []), ...(dr ? [`the denom reward of ${dr}`] : [])];
      return [t(programs.length > 0 ? `Voters changed: now stake in ${programs.join(" and ")}` : "Voters changed")];
    }
    case "EventDaoMembersUpdated": {
      const added = listAttr(e.attrs?.added);
      const removed = listAttr(e.attrs?.removed);
      const parts: HistoryPart[] = [];
      const list = (label: string, addresses: string[]) => {
        if (addresses.length === 0) return;
        if (parts.length > 0) parts.push(t(" · "));
        parts.push(t(`${label}: `));
        addresses.forEach((addr, i) => parts.push(...(i > 0 ? [t(", ")] : []), a(addr)));
      };
      list("Members added", added);
      list("Members removed", removed);
      return parts.length > 0 ? parts : [t("Members changed")];
    }
    default:
      return [t(e.type.replace(/^Event/, "").replace(/([a-z])([A-Z])/g, "$1 $2"))];
  }
}

/** The sentence "Run by …" in words, for the page title tooltip and tests. */
export function statusWords(d: Pick<Dao, "self_governed" | "admin" | "admin_label" | "live">): string {
  if (d.live.pending_admin) return `Admin handover pending to ${shortAddress(d.live.pending_admin)}`;
  if (d.self_governed) return "Self-governed";
  return `Run by ${d.admin_label?.name ?? shortAddress(d.admin)}`;
}

export type DaoTab = "treasury" | "members" | "proposals" | "rules" | "history";

export const DAO_TABS: DaoTab[] = ["treasury", "members", "proposals", "rules", "history"];

/** The `?tab=` of the URL; Treasury (the first tab) for anything else. */
export function parseDaoTab(raw: string | undefined): DaoTab {
  return DAO_TABS.find((t) => t === raw) ?? "treasury";
}
