import type { OriginChain, Token, TokenIbcChannel, TokenSummary } from "@/api/types";

import { multiHop } from "./account";
import { hopsExplainer, isObsolete, OBSOLETE_EXPLAINER, tokenSymbol } from "./tokens";

// The badges of a token's origin, each with the sentence that explains it:
// how the token was born, the chain it came from (and the chain it came
// through), the hops, and the warnings (stranded, obsolete). Pure, so they
// are tested without rendering.

export type BadgeKind = "kind" | "direct" | "home" | "hops" | "stranded" | "obsolete" | "dex";

/**
 * A badge's chip look: `acc` Native, `default` the other kinds, `ch` an
 * origin chain (outlined), `via` the chain a token came through (dashed),
 * `ln` the hops, `ok`/`warn`/`bad` the DEX state and the warnings.
 */
export type BadgeLook = "default" | "acc" | "ch" | "via" | "ln" | "ok" | "warn" | "bad";

export interface ExplainedBadge {
  kind: BadgeKind;
  text: string;
  /** What the badge means, shown on hover and on tap. */
  explainer: string;
  look: BadgeLook;
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
 * cross-chain token its home chain ("from AtomOne" on a multi-hop one) and,
 * for a multi-hop one, the chain BZE received it from ("via Osmosis") and the
 * hops; then the warnings.
 */
export function tokenBadges(t: TokenSummary): ExplainedBadge[] {
  const out: ExplainedBadge[] = [];
  const symbol = tokenSymbol(t);
  switch (t.kind) {
    case "native":
      out.push({ kind: "kind", text: "Native", look: "acc", explainer: `${symbol} is BZE's own coin, minted by the chain itself.` });
      break;
    case "factory":
      out.push({
        kind: "kind",
        text: "Factory",
        look: "default",
        explainer: `${symbol} was created on BZE with the Token Factory, by an account rather than by the chain.`,
      });
      break;
    case "lp":
      out.push({
        kind: "kind",
        text: "DEX pool share",
        look: "default",
        explainer: "A share of a liquidity pool on the BZE DEX, minted to whoever adds liquidity and burned when it is removed.",
      });
      break;
    case "ibc": {
      out.push({
        kind: "kind",
        text: "Cross-chain",
        look: "default",
        explainer: `${symbol} was minted on another chain and sent to BZE over IBC, the protocol Cosmos chains use to move tokens between them.`,
      });
      const hop = multiHop(t);
      if (hop && t.origin_chain) {
        const home = chainName(t.origin_chain, "its home chain");
        out.push({
          kind: "home",
          text: `from ${home}` + (isDeadChain(t.origin_chain) ? " (shut down)" : ""),
          look: "ch",
          logoUrl: t.origin_chain.logo_url,
          explainer: `${symbol} was minted on ${home}, then travelled through ${hop.via ?? "another chain"} before reaching BZE.` + (isDeadChain(t.origin_chain) ? ` ${home} has shut down.` : ""),
        });
      }
      const direct = hop ? t.via_chain : t.origin_chain;
      if (direct) {
        const name = chainName(direct, "another chain");
        out.push({
          kind: "direct",
          text: (hop ? `via ${name}` : name) + (isDeadChain(direct) ? " (shut down)" : ""),
          look: hop ? "via" : "ch",
          logoUrl: direct.logo_url,
          explainer:
            (hop ? `BZE received ${symbol} from ${name}, which it had reached from its home chain first.` : `${symbol} comes from ${name}, sent straight to BZE.`) +
            (isDeadChain(direct) ? ` ${name} has shut down.` : ""),
        });
      }
      if (hop) {
        out.push({
          kind: "hops",
          text: `${hop.hops} hops`,
          look: "ln",
          explainer: hopsExplainer(t) ?? "",
        });
      }
      const stranded = strandedExplainer(t);
      if (stranded) out.push({ kind: "stranded", text: "Stranded", look: "warn", explainer: stranded });
      break;
    }
    default:
      out.push({
        kind: "kind",
        text: "Other",
        look: "default",
        explainer: "A kind of token the explorer does not recognise; its raw denom is in the token's More details.",
      });
  }
  if (isObsolete(t.denom)) out.push({ kind: "obsolete", text: "Obsolete", look: "bad", explainer: OBSOLETE_EXPLAINER });
  return out;
}

/** The badges under a token's name: its origin badges, then whether it trades or is halted on the DEX. */
export function headerChips(t: Token): ExplainedBadge[] {
  const out = tokenBadges(t);
  if (t.halted) {
    out.push({
      kind: "dex",
      text: "Halted on the DEX",
      look: "bad",
      explainer: `Governance halted ${tokenSymbol(t)} on the BZE DEX: its markets take no new orders or swaps.`,
    });
  } else if (t.markets.length > 0) {
    out.push({
      kind: "dex",
      text: "Trading on the DEX",
      look: "ok",
      explainer: `${tokenSymbol(t)} has ${t.markets.length === 1 ? "a market" : `${t.markets.length} markets`} on the BZE DEX.`,
    });
  }
  return out;
}

/** BZE's channel of a token as a chip and the words after it: "Open" and active, or why nothing crosses it. */
export function connectionChip(c: TokenIbcChannel): { look: BadgeLook; text: string; note: string } {
  if (c.state === "closed") return { look: "bad", text: "Closed", note: "it carries no more transfers" };
  if (c.client_status === "expired")
    return { look: "warn", text: "Expired", note: "no relayer kept it updated, so nothing crosses it until governance revives it" };
  if (c.client_status === "frozen") return { look: "warn", text: "Frozen", note: "nothing crosses it until governance revives it" };
  if (c.client_status === "active") return { look: "ok", text: "Open", note: "and active" };
  return { look: "default", text: c.state ? c.state.charAt(0).toUpperCase() + c.state.slice(1) : "Not known yet", note: "" };
}
