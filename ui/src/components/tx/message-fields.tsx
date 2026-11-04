"use client";

import { Box, Grid, HStack, Heading, Text } from "@chakra-ui/react";
import { useMemo } from "react";

import type { Message } from "@/api/types";
import { CopyButton } from "@/components/ui/copy-button";

interface Field {
  path: string;
  value: string;
}

/**
 * One row per leaf of a message body: nested objects as `amount.denom`, arrays as `routes[0].pool_id`.
 * Values stay exactly as the chain sent them: strings verbatim, other scalars as their JSON text,
 * empty arrays and objects as `[]` and `{}`. This is exact because the backend passes the chain's
 * protojson through verbatim, and protojson already encodes 64-bit ints and Decs as strings.
 */
function flatten(value: unknown, path: string, out: Field[]): Field[] {
  if (Array.isArray(value)) {
    if (value.length === 0) out.push({ path, value: "[]" });
    value.forEach((v, i) => flatten(v, `${path}[${i}]`, out));
  } else if (value !== null && typeof value === "object") {
    const entries = Object.entries(value);
    if (entries.length === 0 && path) out.push({ path, value: "{}" });
    for (const [k, v] of entries) flatten(v, path ? `${path}.${k}` : k, out);
  } else {
    out.push({ path, value: typeof value === "string" ? value : JSON.stringify(value) ?? "null" });
  }
  return out;
}

/** "/cosmos.bank.v1beta1.MsgSend" → "MsgSend". */
const shortType = (typeUrl: string) => typeUrl.slice(typeUrl.lastIndexOf(".") + 1) || typeUrl;

// Custom breakpoints are not in Chakra's generated types, so responsive objects using them are hoisted.
const COLUMNS = { base: "minmax(0, 1fr) auto", c2: "minmax(140px, 35%) minmax(0, 1fr) auto" };
const PATH_SPAN = { base: "1 / -1", c2: "auto" };
const PATH_PB = { base: "0", c2: "10px" };
const PATH_LINE = { base: "0", c2: "1px" };
const VALUE_PL = { base: "4", c2: "0" };

function FieldTable({ fields }: { fields: Field[] }) {
  return (
    <Grid as="dl" templateColumns={COLUMNS} m="0" fontFamily="mono" fontSize="12px" columnGap="3">
      {fields.map((f, i) => {
        const line = i < fields.length - 1 ? { borderBottomWidth: "1px", borderColor: "border.panel" } : {};
        return (
          // Paths can collide (`{"a.b": 1, "a": {"b": 2}}`), so rows are keyed by position.
          <Box key={i} display="contents">
            <Text as="dt" gridColumn={PATH_SPAN} pl="4" pt="10px" pb={PATH_PB} color="fg.muted" overflowWrap="anywhere" {...(i < fields.length - 1 ? { borderBottomWidth: PATH_LINE, borderColor: "border.panel" } : {})}>
              {f.path}
            </Text>
            <Text as="dd" m="0" pl={VALUE_PL} py="10px" minW="0" color="fg" whiteSpace="pre-wrap" overflowWrap="anywhere" {...line}>
              {f.value}
            </Text>
            <Box as="dd" m="0" pr="4" py="2" {...line}>
              <CopyButton value={f.value} label={`Copy ${f.path}`} />
            </Box>
          </Box>
        );
      })}
    </Grid>
  );
}

function MessageBlock({ msg }: { msg: Message }) {
  const fields = useMemo(() => (msg.body ? flatten(msg.body, "", []) : null), [msg.body]);
  const n = msg.msg_index + 1;
  return (
    <Box as="section" aria-label={`Message ${n}`} borderTopWidth="1px" borderColor="border.panel">
      <HStack px="4" py="10px" gap="2" wrap="wrap" borderBottomWidth="1px" borderColor="border.panel">
        <Heading as="h3" fontSize="13px" fontWeight="500" whiteSpace="nowrap">
          Message {n} · {shortType(msg.type_url)}
        </Heading>
        <Text as="span" fontFamily="mono" fontSize="12px" color="fg.muted" overflowWrap="anywhere" minW="0">
          {msg.type_url}
        </Text>
      </HStack>
      {fields === null ? (
        <Note>The explorer cannot decode this message type, so its fields are not available.</Note>
      ) : fields.length === 0 ? (
        <Note>This message has no fields.</Note>
      ) : (
        <FieldTable fields={fields} />
      )}
    </Box>
  );
}

const Note = ({ children }: { children: string }) => (
  <Text px="4" py="10px" fontSize="sm" color="fg.muted">
    {children}
  </Text>
);

/** "More details": every message's fields as the chain has them, one block per message in message order. */
export function MessageFields({ messages }: { messages: Message[] }) {
  const ordered = useMemo(() => [...messages].sort((a, b) => a.msg_index - b.msg_index), [messages]);
  return (
    <>
      {ordered.map((m) => (
        <MessageBlock key={m.msg_index} msg={m} />
      ))}
    </>
  );
}
