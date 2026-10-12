import { describe, expect, it } from "vitest";

import type { Block, BlockTx, Status } from "@/api/types";
import { createRenderContext } from "@/render/messages/registry";

import block from "../../test/fixtures/api/v1/blocks/25120440.json";
import status from "../../test/fixtures/api/v1/status.json";

import { blockFees, blockGas, blockNotFound, blockTimeText, sizeText, sumCoins, txOneLine } from "./blocks";

const b = block as unknown as Block;
const s = status as Status;

describe("blockNotFound", () => {
  it("explains a missing height with the status heights", () => {
    expect(blockNotFound(25120451, s)).toBe("not_produced");
    expect(blockNotFound(24998315, s)).toBe("too_old");
    expect(blockNotFound(25000000, s)).toBe("missing");
    expect(blockNotFound(25120450, s)).toBe("missing");
    expect(blockNotFound(24998316, s)).toBe("missing");
  });

  it("calls it missing without the status", () => {
    expect(blockNotFound(99999999, undefined)).toBe("missing");
    expect(blockNotFound(1, { ...s, back_fill: { status: "in_progress", oldest_height: null } })).toBe("missing");
  });
});

describe("block sums", () => {
  it("sums the fees per denom and counts the transactions that paid one", () => {
    expect(blockFees(b)).toEqual({ coins: [{ denom: "ubze", amount: "22000" }], payers: 3 });
    expect(blockFees({ transactions: [] })).toEqual({ coins: [], payers: 0 });
  });

  it("sums coin lists per denom in order of first appearance", () => {
    expect(
      sumCoins([
        [{ denom: "uatom", amount: "1" }],
        [
          { denom: "ubze", amount: "2" },
          { denom: "uatom", amount: "3" },
        ],
        [{ denom: "uzero", amount: "0" }],
      ]),
    ).toEqual([
      { denom: "uatom", amount: "4" },
      { denom: "ubze", amount: "2" },
    ]);
  });

  it("sums the gas the transactions used, null when none reports it", () => {
    expect(blockGas(b)).toBe(81234 + 312500 + 81234);
    const tx = b.transactions[0];
    expect(blockGas({ transactions: [{ ...tx, gas_used: null }] })).toBeNull();
    expect(blockGas({ transactions: [] })).toBeNull();
  });
});

describe("formatting", () => {
  it("shows block time in seconds and size in KB", () => {
    expect(blockTimeText(5810)).toBe("5.8 s");
    expect(blockTimeText(null)).toBe("—");
    expect(sizeText(4096)).toBe("4 KB");
    expect(sizeText(1536)).toBe("1.5 KB");
    expect(sizeText(2_000_000)).toBe("1,953.1 KB");
    expect(sizeText(null)).toBe("—");
  });
});

describe("txOneLine", () => {
  const ctx = createRenderContext({ transfers: b.transfers });
  const [send, delegate] = b.transactions as BlockTx[];

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
