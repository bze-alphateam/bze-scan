import { describe, expect, it } from "vitest";

import type { Block, BlockTx } from "@/api/types";
import { createRenderContext } from "@/render/messages/registry";

import block from "../../test/fixtures/api/v1/blocks/25120440.json";

import { txCounterparty, txOneLine } from "./txs";

const b = block as unknown as Block;
const ctx = createRenderContext({ transfers: b.transfers });
const [send, delegate] = b.transactions as BlockTx[];

const withFirst = (type_url: string, body: Record<string, unknown> | null): BlockTx => ({
  ...send,
  msg_types: [type_url],
  first_message: { type_url, module: null, body },
});

describe("txOneLine", () => {
  it("reads the first message through the registry and counts the others", () => {
    expect(txOneLine(send, ctx)).toBe("Send 10 BZE to bze1m3k…9ds4");
    expect(txOneLine(delegate, ctx)).toMatch(/^Stake 500 BZE with .+ \+ 1 more action$/);
    expect(txOneLine({ ...send, msg_types: [...send.msg_types, "a", "b"] }, ctx)).toBe("Send 10 BZE to bze1m3k…9ds4 + 2 more actions");
  });

  it("falls back to the type in words for an unknown or unreadable message", () => {
    const unknown = { type_url: "/bze.tradebin.MsgCreateOrder", module: "tradebin", body: null };
    expect(txOneLine({ ...send, msg_types: [unknown.type_url], first_message: unknown }, ctx)).not.toBe("");
    expect(txOneLine({ ...send, first_message: { ...send.first_message!, body: null } }, ctx)).not.toBe("");
    expect(txOneLine({ ...send, msg_types: [], first_message: null }, ctx)).toBe("No actions");
  });
});

describe("txCounterparty", () => {
  it("names the recipient of a send and the validator of a stake", () => {
    expect(txCounterparty(send, ctx)).toEqual({ kind: "account", address: send.first_message!.body!.to_address });
    expect(txCounterparty(delegate, ctx)).toEqual({ kind: "validator", operator: delegate.first_message!.body!.validator_address });
  });

  it("names the burner for a burner fund and the community pool for a pool fund", () => {
    expect(txCounterparty(withFirst("/bze.burner.MsgFundBurner", { creator: "bze1x", amount: "5ubze" }), ctx)).toEqual({
      kind: "name",
      name: "Burner",
    });
    expect(txCounterparty(withFirst("/cosmos.distribution.v1beta1.MsgFundCommunityPool", { amount: [] }), ctx)).toEqual({
      kind: "name",
      name: "Community pool",
    });
  });

  it("names nobody for a message without another side, an unknown one or an unreadable body", () => {
    expect(txCounterparty(withFirst("/cosmos.gov.v1.MsgVote", { proposal_id: "4", option: "VOTE_OPTION_YES" }), ctx)).toBeNull();
    expect(txCounterparty(withFirst("/bze.tradebin.MsgCreateOrder", null), ctx)).toBeNull();
    expect(txCounterparty(withFirst("/cosmos.bank.v1beta1.MsgSend", null), ctx)).toBeNull();
    expect(txCounterparty({ ...send, msg_types: [], first_message: null }, ctx)).toBeNull();
  });
});
