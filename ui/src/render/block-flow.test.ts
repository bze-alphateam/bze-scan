import { describe, expect, it } from "vitest";

import type { BlockTransfer } from "@/api/types";

import transfers from "../../test/fixtures/api/v1/blocks/25120440/transfers.json";

import { groupMeta, groupOf, groupTransfers, phraseOf } from "./block-flow";

const rows = transfers.items as BlockTransfer[];

const moduleLabel = (name: string) => ({ name, kind: "module" });
const base: BlockTransfer = {
  tx_index: 0,
  tx_hash: "AB",
  seq: 0,
  msg_index: 0,
  kind: "transfer",
  sender: "bze1alice",
  sender_label: null,
  recipient: "bze1bob",
  recipient_label: null,
  denom: "ubze",
  amount: "1000000",
  symbol: "BZE",
  exponent: 6,
};

describe("groupTransfers", () => {
  it("groups every transfer of a block by what happened, in page order", () => {
    const groups = groupTransfers(rows);
    expect(groups.map((g) => [g.label, g.summary, groupMeta(g)])).toEqual([
      [
        "Fees",
        "0.021 BZE from 3 transactions collected by Fee collector, 0.001 BZE from 1 transaction collected by Burner fee collector",
        "4 transfers · 2 accounts",
      ],
      ["DEX", "600 BZE ↔ 7.5 USDC matched on the DEX", "2 transfers · 2 accounts"],
      ["Staking", "48.2 BZE newly created, 48.2 BZE new rewards, 500 BZE staked, 12.4 BZE rewards claimed", "5 transfers · 1 account"],
      ["Rewards programs", "100 BZE put into rewards programs", "1 transfer · 1 account"],
      ["Burner", "5 BZE burned, 250 BZE set aside for burning", "2 transfers · 1 account"],
      ["Cross-chain", "2 ATOM left for another chain, kept safe by Cross-chain escrow until they come back", "1 transfer · 1 account"],
      ["Governance", "1,000 BZE deposited on proposals", "1 transfer · 1 account"],
      ["Payments", "10 BZE moved between accounts", "1 transfer · 2 accounts"],
      ["Other", "1 HONEY created by Factory", "1 transfer"],
    ]);
    expect(groups.reduce((n, g) => n + g.rows.length, 0)).toBe(rows.length);
  });

  it("marks a group whose every transfer is the chain's own", () => {
    const own = rows.filter((t) => t.tx_index === -1);
    const groups = groupTransfers(own);
    expect(groups.map((g) => [g.group, g.byChain])).toEqual([
      ["dex", true],
      ["staking", true],
      ["burner", true],
    ]);
    expect(groupMeta(groups[1])).toBe("3 transfers · by the chain");
    expect(groupTransfers(rows).some((g) => g.byChain)).toBe(false);
  });

  it("keeps a group's rows in the API's order", () => {
    const staking = groupTransfers(rows).find((g) => g.group === "staking")!;
    expect(staking.rows.map((t) => `${t.tx_index}:${t.seq}`)).toEqual(["-1:0", "-1:1", "-1:2", "3:2", "3:3"]);
  });
});

describe("groupOf", () => {
  it("lets cross-chain and minting win over the other side", () => {
    expect(groupOf({ ...base, sender_label: moduleLabel("DEX"), recipient_label: { name: "Escrow channel-3", kind: "ibc_escrow" } })).toEqual({
      group: "cross_chain",
      side: "in",
    });
    expect(groupOf({ ...base, sender_label: moduleLabel("Cross-chain transfer") })).toEqual({ group: "cross_chain", side: "out" });
    expect(groupOf({ ...base, sender_label: moduleLabel("Mint"), recipient_label: moduleLabel("Fee collector") })).toEqual({
      group: "staking",
      side: "out",
    });
  });

  it("reads the recipient's module first, then the sender's", () => {
    expect(groupOf({ ...base, sender_label: moduleLabel("Fee collector"), recipient_label: moduleLabel("Distribution") }).group).toBe("staking");
    expect(groupOf({ ...base, sender_label: moduleLabel("Burner fee collector"), recipient_label: moduleLabel("Burner") }).group).toBe("burner");
    expect(groupOf({ ...base, recipient_label: moduleLabel("Unbonding tokens pool") })).toEqual({ group: "staking", side: "in" });
    expect(groupOf({ ...base, sender_label: moduleLabel("Governance") })).toEqual({ group: "governance", side: "out" });
  });

  it("calls a transfer between accounts a payment and an unknown module other", () => {
    expect(groupOf({ ...base, recipient_label: { name: "Validator owner", kind: "validator_owner" } })).toEqual({
      group: "payments",
      side: null,
    });
    expect(groupOf({ ...base, recipient_label: moduleLabel("NFT") })).toEqual({ group: "other", side: "in" });
  });
});

describe("phraseOf", () => {
  it("says what each step did", () => {
    expect(phraseOf({ ...base, sender_label: moduleLabel("Unbonding tokens pool") })).toBe("returned after unstaking");
    expect(phraseOf({ ...base, recipient_label: moduleLabel("Burner raffle") })).toBe("put into the raffle");
    expect(phraseOf({ ...base, sender_label: moduleLabel("Burner raffle") })).toBe("won in the raffle");
    expect(phraseOf({ ...base, sender_label: { name: "Escrow", kind: "ibc_escrow" } })).toBe("came back from another chain");
    expect(phraseOf({ ...base, sender_label: moduleLabel("Governance") })).toBe("deposits returned");
    expect(phraseOf({ ...base, sender_label: moduleLabel("DEX") })).toBe("paid out by the DEX");
    expect(phraseOf({ ...base, kind: "burn", recipient: null, sender_label: moduleLabel("Burner") })).toBe("burned");
    // The minted coins moving on from Mint are already counted as created.
    expect(phraseOf({ ...base, sender_label: moduleLabel("Mint"), recipient_label: moduleLabel("Fee collector") })).toBeNull();
  });

  it("reads one-sided DEX groups without the arrow", () => {
    const [dex] = groupTransfers([{ ...base, recipient_label: moduleLabel("DEX") }]);
    expect(dex.summary).toBe("1 BZE sent to the DEX");
  });
});
