import type { Reader } from "./actions";

/** Governance-only chain v8.3.0 messages the message renderers already put in plain words. */
export const READ_AS_MESSAGE: ReadonlySet<string> = new Set([
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
]);

/** `message`: the page draws the action with the message's renderer (title and body). */
export const readMessage: Reader = (type, body) => (READ_AS_MESSAGE.has(type) ? { type: "message", typeUrl: type, body } : null);
