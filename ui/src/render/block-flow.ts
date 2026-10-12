import BigNumber from "bignumber.js";

import type { BlockTransfer, Label } from "@/api/types";
import { formatAmount } from "@/lib/format";

// "Where the funds went in this block": every transfer at a height grouped by
// what happened, read from the module side of each transfer. The grouping uses
// the label names and kinds the backend seeds (backend/internal/labels), never
// module addresses.

export type FlowGroup =
  | "fees"
  | "dex"
  | "staking"
  | "rewards"
  | "burner"
  | "cross_chain"
  | "governance"
  | "payments"
  | "other";

/** The groups in the order the page lists them, with their headings. */
export const FLOW_GROUPS: { group: FlowGroup; label: string }[] = [
  { group: "fees", label: "Fees" },
  { group: "dex", label: "DEX" },
  { group: "staking", label: "Staking" },
  { group: "rewards", label: "Rewards programs" },
  { group: "burner", label: "Burner" },
  { group: "cross_chain", label: "Cross-chain" },
  { group: "governance", label: "Governance" },
  { group: "payments", label: "Payments" },
  { group: "other", label: "Other" },
];

const FEE_COLLECTORS = ["Fee collector", "Burner fee collector", "Stakers fee collector", "CP fee collector"];
const BONDED_POOL = "Bonded tokens pool";
const UNBONDING_POOL = "Unbonding tokens pool";
const DISTRIBUTION = "Distribution";
const MINT = "Mint";
const BURNER = "Burner";
const BLACK_HOLE = "Burner black hole";
const RAFFLE = "Burner raffle";
const IBC_TRANSFER = "Cross-chain transfer";
const RELAYER_FEES = "Cross-chain relayer fees";

/** The group of a module account's label; null for any other label. */
const MODULE_GROUPS = new Map<string, FlowGroup>([
  ...FEE_COLLECTORS.map((n) => [n, "fees"] as const),
  ["DEX", "dex"],
  [BONDED_POOL, "staking"],
  [UNBONDING_POOL, "staking"],
  [DISTRIBUTION, "staking"],
  [MINT, "staking"],
  ["Rewards", "rewards"],
  [BURNER, "burner"],
  [BLACK_HOLE, "burner"],
  [RAFFLE, "burner"],
  [IBC_TRANSFER, "cross_chain"],
  [RELAYER_FEES, "cross_chain"],
  ["Governance", "governance"],
]);

const isModule = (l: Label | null): l is Label => l?.kind === "module" || l?.kind === "ibc_escrow";
const isEscrow = (l: Label | null) => l?.kind === "ibc_escrow";
const named = (l: Label | null, ...names: string[]) => l?.kind === "module" && names.includes(l.name);

function moduleGroup(l: Label | null): FlowGroup | null {
  if (isEscrow(l)) return "cross_chain";
  if (!isModule(l)) return null;
  return MODULE_GROUPS.get(l.name) ?? "other";
}

/**
 * The group of a transfer and its module side: coins going `in` to a module
 * account, coming `out` of one, or neither (`payments`). Cross-chain and the
 * minting of new BZE win wherever they appear; otherwise the recipient's
 * module decides, then the sender's.
 */
export function groupOf(t: BlockTransfer): { group: FlowGroup; side: "in" | "out" | null } {
  const r = t.recipient_label;
  const s = t.sender_label;
  if (isEscrow(r) || named(r, IBC_TRANSFER, RELAYER_FEES)) return { group: "cross_chain", side: "in" };
  if (isEscrow(s) || named(s, IBC_TRANSFER, RELAYER_FEES)) return { group: "cross_chain", side: "out" };
  if (named(r, MINT)) return { group: "staking", side: "in" };
  if (named(s, MINT)) return { group: "staking", side: "out" };
  const byRecipient = moduleGroup(r);
  if (byRecipient) return { group: byRecipient, side: "in" };
  const bySender = moduleGroup(s);
  if (bySender) return { group: bySender, side: "out" };
  return { group: "payments", side: null };
}

/**
 * What a transfer did, as the words after its amount in the group's line
 * ("staked", "rewards claimed"); null for a step the line leaves out (the
 * minted coins moving on from Mint, already counted as created).
 */
export function phraseOf(t: BlockTransfer): string | null {
  const { group, side } = groupOf(t);
  const r = t.recipient_label;
  const s = t.sender_label;
  const name = (side === "in" ? r : s)?.name ?? "";
  if (t.kind === "burn") return "burned";
  switch (group) {
    case "fees":
      return side === "in" ? `collected by ${name}` : `paid out by ${name}`;
    case "dex":
      return side === "in" ? "sent to the DEX" : "paid out by the DEX";
    case "staking":
      if (named(r, MINT) || (t.kind === "mint" && side === "in")) return "newly created";
      if (named(s, MINT)) return null;
      if (named(r, BONDED_POOL)) return "staked";
      if (named(r, UNBONDING_POOL)) return "started unstaking";
      if (named(s, UNBONDING_POOL)) return "returned after unstaking";
      if (named(s, BONDED_POOL)) return "left the staking pool";
      if (named(r, DISTRIBUTION)) return "new rewards";
      return "rewards claimed";
    case "rewards":
      return side === "in" ? "put into rewards programs" : "paid out by rewards programs";
    case "burner":
      if (named(r, BURNER)) return "set aside for burning";
      if (named(r, RAFFLE)) return "put into the raffle";
      if (named(r, BLACK_HOLE)) return "locked in the black hole";
      if (named(s, RAFFLE)) return "won in the raffle";
      return `paid out by ${name}`;
    case "cross_chain":
      if (isEscrow(r)) return `left for another chain, kept safe by ${r?.name} until they come back`;
      if (isEscrow(s)) return "came back from another chain";
      if (t.kind === "mint") return "arrived from another chain";
      if (named(r, RELAYER_FEES)) return "held for relayer fees";
      if (named(s, RELAYER_FEES)) return "paid to relayers";
      return side === "in" ? `sent through ${name}` : `received through ${name}`;
    case "governance":
      return side === "in" ? "deposited on proposals" : "deposits returned";
    case "payments":
      return "moved between accounts";
    case "other":
      if (t.kind === "mint") return `created by ${name}`;
      return side === "in" ? `sent to ${name}` : `paid out by ${name}`;
  }
}

/** Per-denom sums in display units: `600 BZE and 7.5 USDC`. */
function totalsText(rows: BlockTransfer[]): string {
  const sums = new Map<string, { amount: BigNumber; t: BlockTransfer }>();
  for (const t of rows) {
    const n = new BigNumber(t.amount);
    if (!n.isFinite()) continue;
    const prev = sums.get(t.denom);
    sums.set(t.denom, { amount: (prev?.amount ?? new BigNumber(0)).plus(n), t: prev?.t ?? t });
  }
  const parts = [...sums.values()].map(({ amount, t }) =>
    t.symbol != null && t.exponent != null ? formatAmount(amount.toFixed(), t.exponent, t.symbol) : `${amount.toFixed()} ${t.denom}`,
  );
  return parts.length <= 1 ? (parts[0] ?? "") : `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** The fees line: "0.021 BZE from 7 transactions collected by Fee collector", one part per collector. */
function feesLine(rows: BlockTransfer[]): string[] {
  const parts: string[] = [];
  const byPhrase = groupByPhrase(rows);
  for (const [phrase, list] of byPhrase) {
    const txs = new Set(list.filter((t) => t.tx_index >= 0).map((t) => t.tx_index)).size;
    const from = phrase.startsWith("collected") && txs > 0 ? ` from ${plural(txs, "transaction")}` : "";
    parts.push(`${totalsText(list)}${from} ${phrase}`);
  }
  return parts;
}

function groupByPhrase(rows: BlockTransfer[]): Map<string, BlockTransfer[]> {
  const out = new Map<string, BlockTransfer[]>();
  for (const t of rows) {
    const phrase = phraseOf(t);
    if (phrase == null) continue;
    out.set(phrase, [...(out.get(phrase) ?? []), t]);
  }
  return out;
}

/** The group's line after its heading, from its rows' phrases. */
function summaryOf(group: FlowGroup, rows: BlockTransfer[]): string {
  if (group === "fees") return feesLine(rows).join(", ");
  if (group === "dex") {
    const into = rows.filter((t) => groupOf(t).side === "in");
    const out = rows.filter((t) => groupOf(t).side === "out");
    if (into.length > 0 && out.length > 0) return `${totalsText(into)} ↔ ${totalsText(out)} matched on the DEX`;
  }
  return [...groupByPhrase(rows)].map(([phrase, list]) => `${totalsText(list)} ${phrase}`).join(", ");
}

export interface FlowGroupView {
  group: FlowGroup;
  label: string;
  /** The group's transfers in the API's order. */
  rows: BlockTransfer[];
  /** "500 BZE staked, 12.4 BZE rewards claimed". */
  summary: string;
  /** The accounts (not module accounts) the group's transfers touch. */
  accounts: number;
  /** Every row is a step the chain did by itself (block-level, `tx_index` -1). */
  byChain: boolean;
}

/** The block's transfers grouped, groups in page order, empty groups left out. */
export function groupTransfers(transfers: BlockTransfer[]): FlowGroupView[] {
  const rows = new Map<FlowGroup, BlockTransfer[]>();
  for (const t of transfers) {
    const { group } = groupOf(t);
    rows.set(group, [...(rows.get(group) ?? []), t]);
  }
  return FLOW_GROUPS.filter(({ group }) => rows.has(group)).map(({ group, label }) => {
    const list = rows.get(group)!;
    const accounts = new Set<string>();
    for (const t of list) {
      if (t.sender && !isModule(t.sender_label)) accounts.add(t.sender);
      if (t.recipient && !isModule(t.recipient_label)) accounts.add(t.recipient);
    }
    return {
      group,
      label,
      rows: list,
      summary: summaryOf(group, list),
      accounts: accounts.size,
      byChain: list.every((t) => t.tx_index < 0),
    };
  });
}

/** "7 transfers · 3 accounts · by the chain". */
export function groupMeta(g: FlowGroupView): string {
  const parts = [plural(g.rows.length, "transfer")];
  if (g.accounts > 0) parts.push(plural(g.accounts, "account"));
  if (g.byChain) parts.push("by the chain");
  return parts.join(" · ");
}
