import { Box, Text } from "@chakra-ui/react";

import { AccountChip } from "@/components/ui/account-chip";
import { ValidatorChip } from "@/components/ui/validator-chip";

import { coins, coinsText, Fields, humanise } from "./common";
import { renderer, type BodyProps, type MessageRenderer, type RenderContext } from "./types";

const BECH32 = /^bze(valoper)?1[02-9ac-hj-np-z]{38,}$/;

/** One body value: an address as a chip, coins in display units, text as is, anything else as JSON. */
function Value({ value, ctx }: { value: unknown; ctx: RenderContext }) {
  if (typeof value === "string" && BECH32.test(value)) {
    return value.startsWith("bzevaloper1") ? (
      <ValidatorChip operator={value} moniker={ctx.validator(value)} />
    ) : (
      <AccountChip address={value} label={ctx.label(value)} />
    );
  }
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return <Text as="span">{String(value)}</Text>;
  }
  const list = coins(Array.isArray(value) ? value : [value]);
  if (list.length > 0 && list.length === (Array.isArray(value) ? value.length : 1)) {
    return <Text as="span">{coinsText(list, ctx)}</Text>;
  }
  return (
    <Box as="pre" fontFamily="mono" fontSize="xs" whiteSpace="pre-wrap" wordBreak="break-all">
      {JSON.stringify(value, null, 2)}
    </Box>
  );
}

/**
 * Any message no renderer knows: its type in words as the title and its body
 * as a key/value table. Never fails on a body it has not seen.
 */
export const fallbackRenderer: MessageRenderer = renderer({
  category: "other",
  title: (msg) => humanise(msg.type_url),
  Body: function FallbackBody({ msg, ctx }: BodyProps) {
    if (!msg.body) {
      return (
        <Text fontSize="sm" color="fg.muted">
          The explorer cannot read this action&apos;s details; the raw transaction below has them.
        </Text>
      );
    }
    const entries = Object.entries(msg.body).filter(([, v]) => v !== null && v !== "" && !(Array.isArray(v) && v.length === 0));
    return <Fields rows={entries.map(([key, value]) => ({ label: humanise(key.replace(/_(.)/g, (_, c: string) => c.toUpperCase())), value: <Value value={value} ctx={ctx} /> }))} />;
  },
});
