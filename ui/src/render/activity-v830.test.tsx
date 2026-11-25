import { describe, expect, it } from "vitest";

import type { Activity } from "@/api/types";

import { activityLine, lineText } from "./activity";
import { createRenderContext } from "./messages/registry";

const ADDR = "bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4";
const HONEY = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney";

const ctx = createRenderContext({ transfers: [], tokens: new Map([[HONEY, { symbol: "HONEY", exponent: 6, price_usd: null }]]) });
const base: Activity = {
  time: "2026-11-25T10:00:00Z",
  height: 100,
  tx_hash: "AB".repeat(32),
  kind: "other",
  category: "other",
  is_signer: true,
  success: true,
  counterparty: null,
  deltas: [],
  msg_types: [],
  details: null,
  fee: null,
};
const text = (r: Partial<Activity>) => lineText(activityLine({ ...base, ...r }, { address: ADDR, ctx }));

describe("activity sentences of chain v8.3.0", () => {
  it("reads the community pool's trading fees with the token's name", () => {
    const deltas = [{ denom: HONEY, amount: "120000000", symbol: "HONEY", exponent: 6 }];
    expect(text({ kind: "community_pool_fee", is_signer: false, tx_hash: null, deltas })).toBe(
      "Received 120 HONEY of trading fees for the HONEY community pool",
    );
    expect(text({ kind: "community_pool_fee", tx_hash: null })).toBe("Received trading fees for a token's community pool");
  });

  it.each([
    ["dao_create", [], "Created a DAO"],
    ["dao_proposal", ["/bze.dao.MsgSubmitProposal"], "Proposed a change in a DAO"],
    ["dao_proposal", ["/bze.dao.MsgCancelProposal"], "Cancelled a DAO proposal"],
    ["dao_proposal", ["/bze.dao.MsgSettleProposal"], "Settled a DAO proposal"],
    ["dao_proposal", ["/bze.dao.MsgCleanupProposal"], "Cleaned up a DAO proposal"],
    ["dao_proposal", [], "A DAO proposal changed"],
    ["dao_vote", [], "Voted on a DAO proposal"],
    ["dao_admin_execute", [], "Ran actions as a DAO"],
    ["dao_admin", ["/bze.dao.MsgNominateAdmin"], "Nominated an admin of a DAO"],
    ["dao_admin", ["/bze.dao.MsgAcceptAdminNomination"], "Accepted the admin role of a DAO"],
    ["dao_admin", ["/bze.dao.MsgWithdrawAdminNomination"], "Withdrew a DAO admin nomination"],
    ["dao_admin", [], "A DAO's admin changed"],
    ["dao_update", ["/bze.dao.MsgUpdateDaoConfig"], "Changed the rules of a DAO"],
    ["dao_update", ["/bze.dao.MsgUpdateMetadata"], "Changed the name or description of a DAO"],
    ["dao_update", ["/bze.dao.MsgUpdateElectorate"], "Changed the voters of a DAO"],
    ["dao_update", ["/bze.dao.MsgUpdateMembers"], "Changed the members of a DAO"],
    ["dao_update", [], "A DAO was changed"],
    ["guardian", ["/bze.guardian.v1.MsgHaltChain"], "Emergency: scheduled a chain halt"],
    ["guardian", ["/bze.guardian.v1.MsgBlockDenom"], "Emergency: blocked a token from crossing chains"],
    ["guardian", ["/bze.guardian.v1.MsgReopenChannel"], "Reopened a channel"],
    ["guardian", ["/bze.guardian.v1.MsgTightenRateLimit"], "Emergency: lowered an IBC limit"],
    ["guardian", ["/bze.guardian.v1.MsgDisableMessages"], "Emergency: switched off some kinds of actions"],
    ["guardian", ["/bze.guardian.v1.MsgHaltTradingDenoms"], "Emergency: halted trading of tokens"],
    ["guardian", ["/bze.guardian.v1.MsgPauseAutomation"], "Emergency: paused fee conversion and burning"],
    ["guardian", ["/bze.guardian.v1.MsgCloseChannel"], "Emergency: closed a channel"],
    ["guardian", ["/bze.guardian.v1.MsgUnblockDenom"], "Unblocked a token for crossing chains"],
    ["guardian", ["/bze.guardian.v1.MsgResumeAutomation"], "Resumed fee conversion and burning"],
    ["guardian", [], "An emergency measure changed"],
    ["token_admin", ["/bze.tokenfactory.MsgDisableMinting"], "Turned off minting of a token for good"],
    ["token_update", ["/bze.tokenfactory.MsgSetDenomConfig"], "Changed a token's community pool, transfer fee and gas surcharge"],
    ["token_update", ["/bze.tokenfactory.MsgSetDenomBranding"], "Updated a token's branding"],
  ])("%s %j → %s", (kind, msg_types, want) => {
    expect(text({ kind, msg_types })).toBe(want);
  });

  it("marks the guardian's own actions with an Emergency chip, not governance's undo", () => {
    const chips = (r: Partial<Activity>) => activityLine({ ...base, ...r }, { address: ADDR, ctx }).chips.map((c) => c.label);
    expect(chips({ kind: "guardian", msg_types: ["/bze.guardian.v1.MsgHaltChain"] })).toEqual(["Emergency"]);
    expect(chips({ kind: "guardian", msg_types: ["/bze.guardian.v1.MsgHaltChain"], success: false })).toEqual(["Emergency", "Failed"]);
    expect(chips({ kind: "guardian", msg_types: ["/bze.guardian.v1.MsgReopenChannel"] })).toEqual([]);
    expect(chips({ kind: "dao_vote" })).toEqual([]);
  });

  it("reads a block-level guardian row from its event", () => {
    const row = { kind: "guardian", tx_hash: null, msg_types: [], details: [{ type: "bze.guardian.v1.ChannelClosedEvent", attrs: {} }] } as Partial<Activity>;
    expect(text(row)).toBe("Emergency: closed a channel");
    expect(activityLine({ ...base, ...row }, { address: ADDR, ctx }).chips.map((c) => c.label)).toEqual(["Emergency"]);
  });
});
