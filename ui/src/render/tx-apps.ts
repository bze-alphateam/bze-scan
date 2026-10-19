import { burnerRaffleHref, stakingVoteHref, type AppKey } from "@/lib/links";

import { str } from "./messages/common";
import { govProposalId } from "./messages/gov";
import type { RenderMessage } from "./messages/types";

// The BZE apps where a transaction's action can be done again: the explorer is
// read-only, so the tx page links out to them.

/** `Vote on Staking`: `verb` stays text, `name` is the app link. */
export interface AppDestination {
  app: AppKey;
  verb: string;
  name: string;
  href: string;
}

const RAFFLES = new Set(["/bze.burner.MsgJoinRaffle", "/bze.burner.MsgStartRaffle"]);

const GOV_VERBS: Record<string, string> = {};
for (const v of ["v1", "v1beta1"]) {
  GOV_VERBS[`/cosmos.gov.${v}.MsgVote`] = "Vote on";
  GOV_VERBS[`/cosmos.gov.${v}.MsgVoteWeighted`] = "Vote on";
  GOV_VERBS[`/cosmos.gov.${v}.MsgSubmitProposal`] = "View proposal on";
  GOV_VERBS[`/cosmos.gov.${v}.MsgDeposit`] = "View proposal on";
}

function destination(msg: RenderMessage): AppDestination | null {
  if (RAFFLES.has(msg.type_url)) {
    const denom = str(msg, "denom");
    return denom ? { app: "burner", verb: "Open raffle on", name: "Burner", href: burnerRaffleHref(denom) } : null;
  }
  const verb = GOV_VERBS[msg.type_url];
  if (verb) {
    const id = govProposalId(msg);
    return id ? { app: "staking", verb, name: "Staking", href: stakingVoteHref(id) } : null;
  }
  return null;
}

/** One link per app, from the first message that resolves to it, in message order. */
export function appDestinations(messages: RenderMessage[]): AppDestination[] {
  const out: AppDestination[] = [];
  for (const m of messages) {
    const d = destination(m);
    if (d && !out.some((x) => x.app === d.app)) out.push(d);
  }
  return out;
}
