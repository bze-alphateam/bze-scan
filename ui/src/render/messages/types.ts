import type { ComponentType } from "react";

import type { Label, MessageEvent, Transfer } from "@/api/types";
import type { ActionCategory } from "@/components/ui/action-card";

/**
 * A message as the renderers read it: a transaction's message, or one nested
 * in an authz `MsgExec` (which has no events of its own and no index).
 */
export interface RenderMessage {
  type_url: string;
  sender: string | null;
  module: string | null;
  /** Proto JSON, snake_case fields; null when the backend could not decode it. */
  body: Record<string, unknown> | null;
  events: MessageEvent[] | null;
  /** The transaction's message index; null for a nested message. */
  msg_index: number | null;
}

/** The other side of a message: an account, a validator, or a name ("Community pool"). */
export type Counterparty =
  | { kind: "account"; address: string }
  | { kind: "validator"; operator: string }
  | { kind: "name"; name: string };

/** What the API knows about a denom's display. */
export interface DenomInfo {
  symbol: string | null;
  exponent: number | null;
  priceUsd: string | null;
}

/** What renderers may look up beyond the message itself. Nothing in it decodes chain data. */
export interface RenderContext {
  /** The transaction's transfers, in `seq` order. */
  transfers: Transfer[];
  /** Symbol, exponent and price of a denom; nulls when unknown (the raw amount and denom are shown). */
  denom(denom: string): DenomInfo;
  /** A validator's moniker; null when unknown. */
  validator(operator: string): string | null;
  /** An address's label from the transaction's transfers; null when unlabelled. */
  label(address: string): Label | null;
  /** The renderer of a type URL, the fallback for an unknown one (for `MsgExec`'s nested messages). */
  rendererFor(typeUrl: string): MessageRenderer;
}

export interface BodyProps {
  msg: RenderMessage;
  ctx: RenderContext;
}

/** How one message type reads in plain words. */
export interface MessageRenderer {
  category: ActionCategory;
  /** The action card's heading: "Stake 500 BZE with Honeycomb". */
  title(msg: RenderMessage, ctx: RenderContext): string;
  /** One line for lists: "Send · 10 BZE → bze1m3k…9ds4". */
  summary(msg: RenderMessage, ctx: RenderContext): string;
  /** The action card's body. */
  Body: ComponentType<BodyProps>;
  /** Who the signer dealt with, for "signer → counterparty" in lists; absent or null when nobody in particular. */
  counterparty?(msg: RenderMessage): Counterparty | null;
  /** Why one of this message's transfers happened, for the funds flow; the title when absent. */
  reasonFor?(transfer: Transfer, msg: RenderMessage, ctx: RenderContext): string;
}

/** A renderer whose summary defaults to its title. */
export function renderer(
  r: Omit<MessageRenderer, "summary"> & Partial<Pick<MessageRenderer, "summary">>,
): MessageRenderer {
  return { summary: r.title, ...r };
}

/** An account counterparty; null without an address. */
export const account = (address: string | null): Counterparty | null => (address ? { kind: "account", address } : null);

/** A validator counterparty; null without an operator address. */
export const validator = (operator: string | null): Counterparty | null => (operator ? { kind: "validator", operator } : null);
