import type { Proposal } from "@/api/types";

import { entriesOf, humanise, obj, type Action, type Body, type Reader } from "./actions";
import { readCointrunk } from "./cointrunk";
import { readIbc } from "./ibc";
import { readParams } from "./params";
import { readSpend } from "./spend";
import { readUpgrade } from "./upgrade";

const READERS: Reader[] = [readUpgrade, readSpend, readParams, readCointrunk, readIbc];

const LEGACY = "/cosmos.gov.v1.MsgExecLegacyContent";

/** A message, or the legacy content its `MsgExecLegacyContent` carries. */
function unwrap(message: Body): Body {
  const content = message["@type"] === LEGACY ? obj(message.content) : null;
  return content ?? message;
}

function describeOne(message: Body): Action | null {
  const body = unwrap(message);
  const type = typeof body["@type"] === "string" ? body["@type"] : "";
  if (type === "/cosmos.gov.v1beta1.TextProposal") return null;
  for (const read of READERS) {
    const action = read(type, body);
    if (action) return action;
  }
  // A legacy content's title and description repeat the proposal's own.
  const entries = entriesOf(body).filter((e) => body === message || (e.key !== "title" && e.key !== "description"));
  return { type: "other", title: type ? humanise(type) : "Unknown action", typeUrl: type, entries };
}

/**
 * What a proposal does, one action per message (a text proposal's legacy
 * content adds none); `[{ type: "text" }]` when nothing is left.
 */
export function describeProposal(p: Pick<Proposal, "messages">): Action[] {
  const actions = (p.messages ?? []).map((m) => obj(m)).filter((m) => m !== null).map(describeOne).filter((a) => a !== null);
  return actions.length > 0 ? actions : [{ type: "text" }];
}
