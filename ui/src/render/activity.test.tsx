import { readFileSync } from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

import type { Activity, IbcTransfer, Page, TokenSummary } from "@/api/types";
import { createRenderContext } from "@/render/messages/registry";

import page from "../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/activity.json";
import ibcPage from "../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4/ibc.json";
import tokens from "../../test/fixtures/api/v1/tokens.json";

import {
  ACTIVITY_FILTERS,
  actionCategory,
  activityLine,
  deltaLines,
  ibcChip,
  ibcFor,
  lineText,
  parseCategory,
  withoutFee,
} from "./activity";

const ADDR = "bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4";
const OTHER = "bze1q8f7z6x5c4v3d2n0mlkjhgfdsapzxu4a0ex0rq";
const USDC = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4";
const HONEY = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney";
const WDR = "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward";

const rows = (page as Page<Activity>).items;
const transfers = (ibcPage as Page<IbcTransfer>).items;
const ctx = createRenderContext({
  transfers: [],
  tokens: new Map((tokens as Page<TokenSummary>).items.map((t) => [t.denom, t])),
});

const base: Activity = {
  time: "2026-10-12T10:00:00Z",
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
const of = (r: Partial<Activity>): Activity => ({ ...base, ...r });
const bze = (amount: string) => ({ denom: "ubze", amount, symbol: "BZE", exponent: 6 });
const text = (r: Activity, ibc: IbcTransfer | null = null) => lineText(activityLine(r, { address: ADDR, ctx, ibc }));

/**
 * Every kind the backend classifies: `classify.go`'s message and block-event
 * tables plus its `Kind…` constants. Read from the file (vitest runs from
 * ui/) so a kind the backend adds fails here until it has a sentence.
 */
function backendKinds(): string[] {
  const src = readFileSync(path.resolve("../backend/internal/classify/classify.go"), "utf8");
  const consts = new Map([...src.matchAll(/\b(Kind\w+)\s*=\s*"([a-z_]+)"/g)].map((m) => [m[1], m[2]]));
  const kinds = new Set(consts.values());
  for (const m of src.matchAll(/\b(?:msg|event)\("[^"]+",\s*(?:"([a-z_]+)"|(Kind\w+))/g)) {
    kinds.add(m[1] ?? consts.get(m[2])!);
  }
  return [...kinds].sort();
}

describe("activityLine", () => {
  it("has a sentence for every kind the backend classifies", () => {
    const kinds = backendKinds();
    expect(kinds.length).toBeGreaterThan(60);
    expect(kinds).toContain("dex_order_saved");
    expect(kinds).toContain("fee_only");
    const missing = kinds.filter((kind) => {
      if (kind === "other") return false;
      const line = activityLine(of({ kind, msg_types: ["/bze.future.MsgNew"] }), { address: ADDR, ctx });
      return line.fallback || lineText(line).trim() === "";
    });
    expect(missing).toEqual([]);
  });

  it("falls back to the first message type in words for `other` and an unknown kind", () => {
    expect(activityLine(of({ kind: "brand_new", msg_types: ["/bze.future.MsgDoSomething"] }), { address: ADDR, ctx })).toMatchObject({
      parts: ["Do something"],
      fallback: true,
    });
    expect(text(of({ kind: "other", msg_types: [] }))).toBe("Something happened");
  });

  it("reads a send in both directions, with the counterparty as a chip", () => {
    const cp = { address: OTHER, label: null, moniker: null };
    const sent = activityLine(of({ kind: "send", counterparty: cp }), { address: ADDR, ctx });
    expect(sent.parts).toEqual(["Sent to ", { party: cp }]);
    expect(lineText(sent)).toBe("Sent to bze1q8f…x0rq");
    expect(text(of({ kind: "send", is_signer: false, counterparty: { ...cp, label: { name: "Community pool", kind: "module" } } }))).toBe(
      "Received from Community pool",
    );
    expect(text(of({ kind: "send" }))).toBe("Sent tokens to several accounts");
  });

  it("names a validator by moniker and counts the validators of a multi-claim", () => {
    const honeycomb = { address: "bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c", label: null, moniker: "Honeycomb" };
    expect(text(of({ kind: "delegate", counterparty: honeycomb }))).toBe("Staked with Honeycomb");
    expect(text(of({ kind: "claim_rewards", counterparty: honeycomb, msg_types: [WDR] }))).toBe("Claimed rewards from Honeycomb");
    expect(text(of({ kind: "claim_rewards", msg_types: [WDR, WDR, WDR] }))).toBe("Claimed rewards from 3 validators");
  });

  it("reads a block-level DEX fill from its details, from the maker's and the taker's side", () => {
    const fill = rows.find((r) => r.kind === "dex_fill")!;
    expect(fill.tx_hash).toBeNull();
    expect(text(fill)).toBe("Order filled · sold 200 BZE for 2.56 USDC on BZE/USDC");
    expect(lineText(activityLine(fill, { address: OTHER, ctx }))).toBe("Order filled · bought 200 BZE for 2.56 USDC on BZE/USDC");

    const second = { ...fill.details![0], attrs: { ...fill.details![0].attrs, market_id: `${HONEY}/ubze`, amount: "5000000", price: "2" } };
    expect(text({ ...fill, details: [...fill.details!, second] })).toBe("Order filled · sold 200 BZE for 2.56 USDC on BZE/USDC + 1 more");
    expect(text(of({ kind: "dex_fill" }))).toBe("Sent orders to fill on the DEX");
  });

  it("reads the order book's chain v8.3.0 outcomes", () => {
    const refunded = {
      type: "bze.tradebin.OrderMessageRefundedEvent",
      attrs: { owner: ADDR, market_id: `ubze/${USDC}`, amount: "16087378657", refund: "16087378657ubze", reason: "no_match" },
    };
    expect(text(of({ kind: "dex_refund", tx_hash: null, details: [refunded] }))).toBe("Refunded · 16,087.378657 BZE on BZE/USDC");
    const dust = { ...refunded, attrs: { ...refunded.attrs, refund: "" } };
    expect(text(of({ kind: "dex_refund", tx_hash: null, details: [dust] }))).toBe("Refund too small to send back on BZE/USDC");
    expect(text(of({ kind: "dex_refund" }))).toBe("Refunded by the DEX");
    expect(text(of({ kind: "dex_cancel_skipped", tx_hash: null }))).toBe("Cancel skipped, the order was gone");
    expect(text(of({ kind: "dex_failed", tx_hash: null }))).toBe("The DEX could not process an order yet");
  });

  it("reads an order resting on the book, a chain-made cancel and a swap", () => {
    const saved = {
      type: "bze.tradebin.OrderSavedEvent",
      attrs: { owner: ADDR, market_id: `ubze/${USDC}`, amount: "400000000", order_type: "sell", price: "0.0128" },
    };
    expect(text(of({ kind: "dex_order_saved", tx_hash: null, details: [saved] }))).toBe("Order placed on the book · 400 BZE still waiting");
    const cancelled = { type: "bze.tradebin.OrderCanceledEvent", attrs: { ...saved.attrs, id: "12" } };
    expect(text(of({ kind: "dex_cancel", tx_hash: null, details: [cancelled] }))).toBe("Order cancelled on BZE/USDC");
    expect(text(of({ kind: "dex_cancel" }))).toBe("Cancelled an order");
    const swap = of({
      kind: "dex_swap",
      deltas: [bze("-50002000"), { denom: USDC, amount: "640000", symbol: "USDC", exponent: 6 }],
      fee: [bze("2000")],
    });
    expect(text(swap)).toBe("Swapped 50 BZE for 0.64 USDC");
  });

  it("reads a raffle win from the deltas, else from the event", () => {
    const winner = { type: "bze.burner.RaffleWinnerEvent", attrs: { winner: ADDR, denom: "ubze", amount: "120000000" } };
    expect(text(of({ kind: "raffle_won", tx_hash: null, details: [winner] }))).toBe("Won the raffle · 120 BZE");
    expect(text(of({ kind: "raffle_won", tx_hash: null, details: [winner], deltas: [bze("121000000")] }))).toBe("Won the raffle · 121 BZE");
    const trading = {
      type: "bze.rewards.TradingRewardDistributionEvent",
      attrs: { reward_id: "4", prize_amount: "12000000", prize_denom: HONEY, winners: [ADDR] },
    };
    expect(text(of({ kind: "reward_won", tx_hash: null, details: [trading] }))).toBe("Won trading rewards · 12 HONEY");
  });

  it("reads a completed unstaking with what came back", () => {
    const done = { type: "complete_unbonding", attrs: { amount: "10000000ubze", delegator: ADDR, validator: "bzevaloper1x" } };
    expect(text(of({ kind: "unbonding_completed", tx_hash: null, details: [done] }))).toBe("Unstaking completed · 10 BZE back in your account");
    expect(text(of({ kind: "unbonding_completed", tx_hash: null }))).toBe("Unstaking completed · back in your account");
  });

  it("marks a failed transaction: its first message in words and a red Failed chip", () => {
    const failed = rows.find((r) => r.kind === "fee_only")!;
    expect(activityLine(failed, { address: ADDR, ctx })).toEqual({
      parts: ["Send"],
      chips: [{ label: "Failed", palette: "red" }],
      fallback: false,
    });
    // A fee granter's row on a successful transaction.
    expect(text(of({ kind: "fee_only", is_signer: false, msg_types: ["/cosmos.bank.v1beta1.MsgSend"] }))).toBe("Paid the fee of: Send");
  });

  it("names the other chain and the status of a cross-chain transfer", () => {
    const outRow = of({ kind: "ibc_out", tx_hash: "CC".repeat(32), category: "cross_chain" });
    const t: IbcTransfer = { ...transfers[1], created_tx_hash: "cc".repeat(32) };
    expect(ibcFor(outRow, [t])).toBe(t);
    expect(text(outRow, t)).toBe("Cross-chain transfer to Cosmos Hub");
    expect(text(outRow)).toBe("Cross-chain transfer to another chain");

    const chips = (status: IbcTransfer["status"], expired = false) =>
      activityLine(outRow, { address: ADDR, ctx, ibc: { ...t, status, expired } }).chips.map((c) => `${c.label}:${c.palette}`);
    expect(chips("pending")).toEqual(["Pending:orange"]);
    expect(chips("pending", true)).toEqual(["Expired:orange"]);
    expect(chips("delivered")).toEqual(["Delivered:green"]);
    expect(chips("refunded_rejected")).toEqual(["Refunded:red"]);
    expect(chips("refunded_timeout")).toEqual(["Refunded:red"]);
  });

  it("matches inbound transfers and refunds to their transactions", () => {
    const inRow = rows.find((r) => r.kind === "ibc_in")!;
    const t = ibcFor(inRow, transfers);
    expect(t?.direction).toBe("in");
    expect(text(inRow, t)).toBe("Cross-chain transfer from Noble");
    expect(ibcChip(t!)).toEqual({ label: "Received", palette: "green" });
    expect(ibcChip({ ...t!, status: "rejected" })).toEqual({ label: "Rejected", palette: "red" });

    const refund = of({ kind: "ibc_refund", tx_hash: transfers[1].result_tx_hash });
    expect(text(refund, ibcFor(refund, transfers))).toBe("Refunded cross-chain transfer to Cosmos Hub");
    expect(ibcFor(of({ kind: "send" }), transfers)).toBeNull();
    expect(ibcFor(of({ kind: "ibc_out", tx_hash: null }), transfers)).toBeNull();
  });
});

describe("deltaLines", () => {
  it("shows what moved apart from the fee, signed, and the fee on its own line", () => {
    const sent = rows.find((r) => r.kind === "send" && r.is_signer)!;
    expect(withoutFee(sent)).toEqual([bze("-10000000")]);
    expect(deltaLines(sent, ctx)).toEqual([
      { text: "− 10 BZE", positive: false, fee: false },
      { text: "− 0.002 BZE fee", positive: false, fee: true },
    ]);
    const received = rows.find((r) => r.kind === "send" && !r.is_signer)!;
    expect(deltaLines(received, ctx)).toEqual([{ text: "+ 25 HONEY", positive: true, fee: false }]);
  });

  it("leaves only the fee for a failed transaction, nothing for a row that moved nothing", () => {
    const failed = rows.find((r) => r.kind === "fee_only")!;
    expect(deltaLines(failed, ctx).map((l) => l.text)).toEqual(["− 0.002 BZE fee"]);
    expect(deltaLines(of({}), ctx)).toEqual([]);
  });

  it("formats a denom without display data through the context, else raw", () => {
    expect(deltaLines(of({ deltas: [{ denom: USDC, amount: "1500000", symbol: null, exponent: null }] }), ctx)[0].text).toBe("+ 1.5 USDC");
    expect(deltaLines(of({ deltas: [{ denom: "uunknown", amount: "-7", symbol: null, exponent: null }] }), ctx)[0].text).toBe(
      "− 7 uunknown",
    );
  });
});

describe("category chips", () => {
  it("lists All and the API's ten categories, and reads ?category=", () => {
    expect(ACTIVITY_FILTERS.map((f) => `${f.label}=${f.value ?? "all"}`)).toEqual([
      "All=all",
      "Sent=sent",
      "Received=received",
      "Staking=staking",
      "DEX=dex",
      "Rewards=rewards",
      "Governance=governance",
      "Tokens=tokens",
      "Burner=burner",
      "Cross-chain=cross_chain",
      "Other=other",
    ]);
    expect(parseCategory("dex")).toBe("dex");
    expect(parseCategory(["dex"])).toBeNull();
    expect(parseCategory("everything")).toBeNull();
    expect(parseCategory(undefined)).toBeNull();
    expect(actionCategory("received")).toBe("send");
    expect(actionCategory("cross_chain")).toBe("cross_chain");
  });
});
