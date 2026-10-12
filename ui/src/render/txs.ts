import type { TxSummary } from "@/api/types";
import { fallbackRenderer } from "@/render/messages/fallback";
import type { Counterparty, RenderContext, RenderMessage } from "@/render/messages/types";

/** What a list row knows of a transaction: a block's transaction or a transactions list item. */
export type TxLine = Pick<TxSummary, "msg_types" | "first_message" | "signer">;

/** The transaction's first message as the renderers read it; null without one. */
function firstMessage(tx: TxLine): RenderMessage | null {
  const first = tx.first_message;
  const typeUrl = first?.type_url ?? tx.msg_types[0];
  if (!typeUrl) return null;
  return {
    type_url: typeUrl,
    sender: tx.signer,
    module: first?.module ?? null,
    body: first?.body ?? null,
    events: null,
    msg_index: 0,
  };
}

/**
 * What a transaction did, in one line: its first message's title through the
 * registry (the fallback's for an unknown type), and "+ N more actions" when
 * it has more.
 */
export function txOneLine(tx: TxLine, ctx: RenderContext): string {
  const msg = firstMessage(tx);
  if (!msg) return "No actions";
  const more = tx.msg_types.length - 1;
  const suffix = more > 0 ? ` + ${more} more action${more === 1 ? "" : "s"}` : "";
  let title: string;
  try {
    title = ctx.rendererFor(msg.type_url).title(msg, ctx);
  } catch {
    // A body a renderer did not expect: its type in words.
    title = fallbackRenderer.title(msg, ctx);
  }
  return title + suffix;
}

// Chain messages the registry does not read yet, whose other side is a module.
const MODULE_COUNTERPARTIES: Record<string, string> = {
  "/bze.burner.MsgFundBurner": "Burner",
};

/**
 * Who the signer dealt with in the first message: the recipient of a send,
 * the validator of a stake, "Burner" for a burner fund; null when the
 * renderer names nobody.
 */
export function txCounterparty(tx: TxLine, ctx: RenderContext): Counterparty | null {
  const msg = firstMessage(tx);
  if (!msg) return null;
  const name = MODULE_COUNTERPARTIES[msg.type_url];
  if (name) return { kind: "name", name };
  try {
    return ctx.rendererFor(msg.type_url).counterparty?.(msg) ?? null;
  } catch {
    return null;
  }
}
