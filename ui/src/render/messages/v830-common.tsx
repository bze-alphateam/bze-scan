import { Badge, Box, Stack, Text } from "@chakra-ui/react";
import BigNumber from "bignumber.js";

import { Account, obj } from "./common";
import type { MessageRenderer, RenderContext, RenderMessage } from "./types";

// Helpers the chain v8.3.0 renderers (dao, guardian, ratelimit, tokenfactory,
// tradebin pools) share.

/** A ratio (`0.05`) as a percentage without trailing zeros: `5%`. */
export function ratioPct(ratio: string | number | null | undefined): string | null {
  if (ratio == null || ratio === "") return null;
  const v = new BigNumber(ratio).times(100);
  return v.isFinite() ? `${v.decimalPlaces(2).toFixed()}%` : null;
}

/** A whole percentage the chain stores as an integer (`2`): `2%`. */
export function wholePct(value: string | number | null | undefined): string | null {
  if (value == null || value === "") return null;
  const v = new BigNumber(value);
  return v.isFinite() ? `${v.decimalPlaces(2).toFixed()}%` : null;
}

/** A string list in an event attribute: a real array, a JSON array or a comma list. */
export function listAttr(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String).filter((x) => x !== "");
  if (typeof value !== "string" || value === "") return [];
  if (value.startsWith("[")) {
    try {
      const parsed: unknown = JSON.parse(value);
      if (Array.isArray(parsed)) return parsed.map(String).filter((x) => x !== "");
    } catch {
      // not JSON: a plain list
    }
  }
  return value.split(",").map((x) => x.trim()).filter((x) => x !== "");
}

/** Every event of `type` of the message. */
export function eventsOf(msg: RenderMessage, type: string): Record<string, unknown>[] {
  return (msg.events ?? []).filter((e) => e.type === type).map((e) => e.attrs);
}

/** The messages a body's `msgs` carry (each names its type in `@type`), as renderable messages. */
export function nestedMessages(msg: RenderMessage, events: RenderMessage["events"] = null): RenderMessage[] {
  const msgs = msg.body?.msgs;
  if (!Array.isArray(msgs)) return [];
  return msgs.map((m) => {
    const { "@type": type, ...body } = obj(m) ?? {};
    return { type_url: typeof type === "string" ? type : "", sender: null, module: null, body, events, msg_index: null };
  });
}

/** The nested messages in plain words, each with its own body (as a MsgExec does). */
export function NestedList({ list, ctx }: { list: RenderMessage[]; ctx: RenderContext }) {
  return (
    <Stack gap="4">
      {list.map((m, i) => {
        const r = ctx.rendererFor(m.type_url);
        return (
          <Box key={i} borderStartWidth="2px" borderColor="border.panel" ps="4" data-testid="nested-message">
            <Text fontWeight="semibold" mb="2">
              {list.length > 1 ? `${i + 1}. ` : ""}
              {r.title(m, ctx)}
            </Text>
            <r.Body msg={m} ctx={ctx} />
          </Box>
        );
      })}
    </Stack>
  );
}

/** The orange badge on every emergency action. */
export function EmergencyBadge() {
  return (
    <Badge colorPalette="orange" variant="subtle" data-testid="emergency-badge" alignSelf="flex-start">
      Emergency
    </Badge>
  );
}

/** An account chip when the value is an address. */
export function AddressOrText({ value, ctx }: { value: string | null; ctx: RenderContext }) {
  if (!value) return null;
  return value.startsWith("bze1") ? <Account address={value} ctx={ctx} /> : <Text as="span">{value}</Text>;
}

/** Register a renderer table's entries under a prefix. */
export function under(prefix: string, table: Record<string, MessageRenderer>): Record<string, MessageRenderer> {
  return Object.fromEntries(Object.entries(table).map(([k, v]) => [`${prefix}${k}`, v]));
}
