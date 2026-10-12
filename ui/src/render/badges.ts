import type { OriginChain, Token, TokenIbcChannel, TokenSummary } from "@/api/types";

import { multiHop } from "./account";
import { hopsExplainer, isObsolete, OBSOLETE_EXPLAINER, tokenSymbol } from "./tokens";

// The badges of a token's origin, each with the sentence that explains it:
// how the token was born, the chain it came from (and the chain it came
// through), the hops, and the warnings (stranded, obsolete). Pure, so they
// are tested without rendering.

export type BadgeKind = "kind" | "direct" | "home" | "hops" | "stranded" | "obsolete" | "dex";

export interface ExplainedBadge {
  kind: BadgeKind;
  text: string;
  /** What the badge means, shown on hover and on tap. */
  explainer: string;
  tone: "blue" | "purple" | "cyan" | "orange" | "red" | "green";
  variant: "subtle" | "outline" | "solid";
  logoUrl?: string | null;
}

const chainName = (c: OriginChain | null | undefined, fallback: string) => c?.name || c?.chain_id || fallback;

/** A chain the chain registry lists as shut down. */
export const isDeadChain = (c: OriginChain | null | undefined) => c?.status === "killed";

type StrandedInput = Pick<TokenSummary, "kind" | "stranded" | "ibc_channel" | "origin_chain" | "via_chain" | "symbol" | "denom">;

/**
 * Why an IBC token can no longer travel back: its channel is closed, BZE's
 * light client of the other chain expired or froze, or a chain on its way
 * shut down. Null for a token that is not stranded.
 */
export function strandedExplainer(t: StrandedInput): string | null {
  if (t.kind !== "ibc" || !t.stranded) return null;
  const direct = t.via_chain ?? t.origin_chain;
  const next = chainName(direct, "the other chain");
  const channel = t.ibc_channel?.channel_id ?? "its channel";
  const reasons: string[] = [];
  if (isDeadChain(t.origin_chain)) reasons.push(`${chainName(t.origin_chain, "Its home chain")}, where it was minted, has shut down.`);
  if (t.via_chain && isDeadChain(t.via_chain)) reasons.push(`${chainName(t.via_chain, "The chain it came through")}, the chain it came through, has shut down.`);
  if (t.ibc_channel?.state === "closed") reasons.push(`BZE's channel to ${next} (${channel}) is closed.`);
  const client = t.ibc_channel?.client_status;
  if (client === "expired" || client === "frozen") {
    reasons.push(
      `BZE's connection to ${next} (${channel}) has ${client === "expired" ? "expired: no relayer updated it in time" : "been frozen"}, ` +
        `so nothing crosses it until a governance proposal revives it.`,
    );
  }
  const dead = isDeadChain(t.origin_chain) || isDeadChain(t.via_chain);
  const outcome = dead
    ? `The ${tokenSymbol(t)} on BZE can never be sent back`
    : `Until then, the ${tokenSymbol(t)} on BZE cannot be sent back`;
  return `${reasons.join(" ")} ${outcome}; it can still move between BZE accounts.`;
}

/**
 * The badges of a token, in reading order: how it was born; for a
 * cross-chain token the chain BZE received it from (same color) and, for a
 * multi-hop one, its home chain further away (another color) with the hops;
 * then the warnings, highlighted.
 */
export function tokenBadges(t: TokenSummary): ExplainedBadge[] {
  const out: ExplainedBadge[] = [];
  const symbol = tokenSymbol(t);
  switch (t.kind) {
    case "native":
      out.push({ kind: "kind", text: "Native", tone: "blue", variant: "subtle", explainer: `${symbol} is BZE's own coin, minted by the chain itself.` });
      break;
    case "factory":
      out.push({
        kind: "kind",
        text: "Factory",
        tone: "blue",
        variant: "subtle",
        explainer: `${symbol} was created on BZE with the Token Factory, by an account rather than by the chain.`,
      });
      break;
    case "lp":
      out.push({
        kind: "kind",
        text: "DEX pool share",
        tone: "blue",
        variant: "subtle",
        explainer: "A share of a liquidity pool on the BZE DEX, minted to whoever adds liquidity and burned when it is removed.",
      });
      break;
    case "ibc": {
      out.push({
        kind: "kind",
        text: "Cross-chain",
        tone: "blue",
        variant: "subtle",
        explainer: `${symbol} was minted on another chain and sent to BZE over IBC, the protocol Cosmos chains use to move tokens between them.`,
      });
      const hop = multiHop(t);
      const direct = hop ? t.via_chain : t.origin_chain;
      if (direct) {
        const name = chainName(direct, "another chain");
        out.push({
          kind: "direct",
          text: name + (isDeadChain(direct) ? " (shut down)" : ""),
          tone: "blue",
          variant: "subtle",
          logoUrl: direct.logo_url,
          explainer:
            (hop ? `BZE received ${symbol} from ${name}, which it had reached from its home chain first.` : `${symbol} comes from ${name}, sent straight to BZE.`) +
            (isDeadChain(direct) ? ` ${name} has shut down.` : ""),
        });
      }
      if (hop && t.origin_chain) {
        const home = chainName(t.origin_chain, "its home chain");
        out.push({
          kind: "home",
          text: home + (isDeadChain(t.origin_chain) ? " (shut down)" : ""),
          tone: "purple",
          variant: "subtle",
          logoUrl: t.origin_chain.logo_url,
          explainer: `${symbol} was minted on ${home}, then travelled through ${hop.via ?? "another chain"} before reaching BZE.` + (isDeadChain(t.origin_chain) ? ` ${home} has shut down.` : ""),
        });
      }
      if (hop) {
        out.push({
          kind: "hops",
          text: `${hop.hops} hops`,
          tone: "cyan",
          variant: "outline",
          explainer: hopsExplainer(t) ?? "",
        });
      }
      const stranded = strandedExplainer(t);
      if (stranded) out.push({ kind: "stranded", text: "Stranded", tone: "orange", variant: "solid", explainer: stranded });
      break;
    }
    default:
      out.push({
        kind: "kind",
        text: "Other",
        tone: "blue",
        variant: "subtle",
        explainer: "A kind of token the explorer does not recognise; its raw denom is in the token's More details.",
      });
  }
  if (isObsolete(t.denom)) out.push({ kind: "obsolete", text: "Obsolete", tone: "red", variant: "solid", explainer: OBSOLETE_EXPLAINER });
  return out;
}

/** The badges under a token's name: its origin badges, then whether it trades or is halted on the DEX. */
export function headerChips(t: Token): ExplainedBadge[] {
  const out = tokenBadges(t);
  if (t.halted) {
    out.push({
      kind: "dex",
      text: "Halted on the DEX",
      tone: "red",
      variant: "subtle",
      explainer: `Governance halted ${tokenSymbol(t)} on the BZE DEX: its markets take no new orders or swaps.`,
    });
  } else if (t.markets.length > 0) {
    out.push({
      kind: "dex",
      text: "Trading on the DEX",
      tone: "green",
      variant: "subtle",
      explainer: `${tokenSymbol(t)} has ${t.markets.length === 1 ? "a market" : `${t.markets.length} markets`} on the BZE DEX.`,
    });
  }
  return out;
}

/** Whether BZE's channel of a token still carries transfers, as far as the explorer knows. */
export const connectionHealthy = (c: TokenIbcChannel) =>
  c.state !== "closed" && c.client_status !== "expired" && c.client_status !== "frozen";

/** BZE's channel of a token in words: open, or why nothing crosses it. */
export function connectionText(c: TokenIbcChannel): string {
  if (c.state === "closed") return "closed: it carries no more transfers";
  if (c.client_status === "expired") return "expired: no relayer kept it updated, so nothing crosses it until governance revives it";
  if (c.client_status === "frozen") return "frozen: nothing crosses it until governance revives it";
  if (c.client_status === "active") return "open and active";
  return c.state ?? "state not known yet";
}
