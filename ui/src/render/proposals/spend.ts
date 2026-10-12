import { coins, text, type Reader } from "./actions";

/** `community_pool_spend`: coins from the community pool to a recipient. */
export const readSpend: Reader = (type, body) =>
  type === "/cosmos.distribution.v1beta1.MsgCommunityPoolSpend" || type === "/cosmos.distribution.v1beta1.CommunityPoolSpendProposal"
    ? { type: "spend", recipient: text(body, "recipient"), amount: coins(body.amount) }
    : null;
