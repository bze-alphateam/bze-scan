import { describe, expect, it } from "vitest";

import { describeProposal } from "./describe";

const A = "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw";

describe("governance-only messages of chain v8.3.0", () => {
  it.each([
    "/bze.tokenfactory.MsgSetDenomAdmin",
    "/bze.tradebin.MsgUpdatePools",
    "/ratelimit.v1.MsgAddRateLimit",
    "/ratelimit.v1.MsgUpdateRateLimit",
    "/ratelimit.v1.MsgRemoveRateLimit",
    "/ratelimit.v1.MsgResetRateLimit",
    "/bze.guardian.v1.MsgUnblockDenom",
    "/bze.guardian.v1.MsgResumeAutomation",
    "/bze.guardian.v1.MsgReopenChannel",
    "/bze.guardian.v1.MsgUpdateParams",
    "/bze.dao.MsgUpdateParams",
  ])("reads %s as a message the renderers put in words", (type) => {
    const body = { "@type": type, authority: A };
    expect(describeProposal({ messages: [body] })).toEqual([{ type: "message", typeUrl: type, body }]);
  });

  it("no longer tables the guardian's and the DAO's parameters", () => {
    const [action] = describeProposal({ messages: [{ "@type": "/bze.guardian.v1.MsgUpdateParams", authority: A, params: { guardian: A } }] });
    expect(action.type).toBe("message");
  });

  it("leaves other modules' parameter changes to the parameter table", () => {
    const [action] = describeProposal({ messages: [{ "@type": "/bze.burner.MsgUpdateParams", authority: A, params: { a: 1 } }] });
    expect(action).toMatchObject({ type: "params", module: "burner" });
  });
});
