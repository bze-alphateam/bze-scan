import type { Label, Transfer } from "@/api/types";

import { authzRenderers } from "./authz";
import { bankRenderers } from "./bank";
import { distributionRenderers } from "./distribution";
import { fallbackRenderer } from "./fallback";
import { govRenderers } from "./gov";
import { ibcRenderers } from "./ibc";
import { stakingRenderers } from "./staking";
import type { DenomInfo, MessageRenderer, RenderContext } from "./types";

/** Every message type the explorer reads in plain words, by type URL. */
export const registry: ReadonlyMap<string, MessageRenderer> = new Map(
  Object.entries({
    ...bankRenderers,
    ...stakingRenderers,
    ...distributionRenderers,
    ...govRenderers,
    ...ibcRenderers,
    ...authzRenderers,
  }),
);

/** The renderer of a type URL; the fallback for any other. */
export function rendererFor(typeUrl: string): MessageRenderer {
  return registry.get(typeUrl) ?? fallbackRenderer;
}

export interface ContextSources {
  transfers: Transfer[];
  /** The tokens list by denom (symbol, exponent, price). */
  tokens?: ReadonlyMap<string, { symbol: string | null; exponent: number; price_usd: string | null }>;
  /** The validators list by operator address. */
  validators?: ReadonlyMap<string, { moniker: string }>;
}

/**
 * The renderers' context: denoms from the transaction's own transfers first
 * (the API resolved them), then the tokens list; labels from the transfers.
 */
export function createRenderContext({ transfers, tokens, validators }: ContextSources): RenderContext {
  const denoms = new Map<string, DenomInfo>();
  const labels = new Map<string, Label>();
  for (const t of transfers) {
    if (t.symbol != null && t.exponent != null && !denoms.has(t.denom)) {
      denoms.set(t.denom, { symbol: t.symbol, exponent: t.exponent, priceUsd: null });
    }
    if (t.sender && t.sender_label) labels.set(t.sender, t.sender_label);
    if (t.recipient && t.recipient_label) labels.set(t.recipient, t.recipient_label);
  }
  return {
    transfers,
    denom(denom) {
      const token = tokens?.get(denom);
      const own = denoms.get(denom);
      return {
        symbol: own?.symbol ?? token?.symbol ?? null,
        exponent: own?.exponent ?? (token?.symbol != null ? token.exponent : null),
        priceUsd: token?.price_usd ?? null,
      };
    },
    validator: (operator) => validators?.get(operator)?.moniker ?? null,
    label: (address) => labels.get(address) ?? null,
    rendererFor,
  };
}
