import type { Coin } from "@/api/types";

import { coins, humanise, obj } from "../messages/common";

// "What this proposal does": each proposal message read into an action the
// page draws in plain words. One reader per proposal kind (upgrade.ts,
// spend.ts, params.ts, cointrunk.ts, ibc.ts), each knowing its type URLs, both
// the messages and the legacy contents a `MsgExecLegacyContent` carries; a
// message none of them reads is an `other` action, its fields as a table.

/** A key and its raw value, for the parameter and fallback tables. */
export interface Entry {
  key: string;
  value: unknown;
}

export type Action =
  | { type: "upgrade"; name: string; height: number | null }
  | { type: "cancel_upgrade" }
  | { type: "spend"; recipient: string | null; amount: Coin[] }
  /** `whole`: a `MsgUpdateParams`, which sets every parameter of the module. */
  | { type: "params"; module: string; entries: Entry[]; whole: boolean }
  | { type: "publisher"; add: boolean; name: string | null; address: string | null }
  | { type: "domain"; add: boolean; domain: string | null }
  | { type: "ibc_client"; subject: string | null; substitute: string | null }
  | { type: "text" }
  | { type: "other"; title: string; typeUrl: string; entries: Entry[] };

/** A message body as proto JSON. */
export type Body = Record<string, unknown>;

/** A reader: the action of a body whose `@type` it knows, else null. */
export type Reader = (type: string, body: Body) => Action | null;

/** A string field, or null for a missing or empty one. */
export function text(body: Body, key: string): string | null {
  const v = body[key];
  return typeof v === "string" && v !== "" ? v : typeof v === "number" ? String(v) : null;
}

/** A positive integer field (heights come as strings), or null. */
export function height(value: unknown): number | null {
  const n = typeof value === "string" ? Number(value) : typeof value === "number" ? value : NaN;
  return Number.isSafeInteger(n) && n > 0 ? n : null;
}

/** The body's fields other than its type and the gov module's `authority`. */
export function entriesOf(body: Body): Entry[] {
  return Object.entries(body)
    .filter(([key, v]) => key !== "@type" && key !== "authority" && v != null && v !== "" && !(Array.isArray(v) && v.length === 0))
    .map(([key, value]) => ({ key, value }));
}

export { coins, humanise, obj };
