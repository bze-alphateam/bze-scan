import { Box, Grid, HStack, Text } from "@chakra-ui/react";
import BigNumber from "bignumber.js";
import type { ReactNode } from "react";
import { LuArrowRight } from "react-icons/lu";

import type { Coin, Transfer } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatAmount, formatUsd, shortAddress } from "@/lib/format";

import type { RenderContext, RenderMessage } from "./types";

// Helpers every renderer shares: safe readers of a proto JSON body, amounts
// in display units, names of accounts and validators, and the body atoms.

/** A string field of the body, or null. */
export function str(msg: RenderMessage, key: string): string | null {
  const v = msg.body?.[key];
  return typeof v === "string" && v !== "" ? v : typeof v === "number" ? String(v) : null;
}

/** An object field of the body, or null. */
export function obj(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

function asCoin(value: unknown): Coin | null {
  const o = obj(value);
  return o && typeof o.denom === "string" && (typeof o.amount === "string" || typeof o.amount === "number")
    ? { denom: o.denom, amount: String(o.amount) }
    : null;
}

/** A `Coin` field of the body, or null. */
export function coin(msg: RenderMessage, key: string): Coin | null {
  return asCoin(msg.body?.[key]);
}

/** A `repeated Coin` field (any value that holds coins), the malformed entries skipped. */
export function coins(value: unknown): Coin[] {
  return Array.isArray(value) ? value.map(asCoin).filter((c): c is Coin => c !== null) : [];
}

/** The first value of `key` in the message's first event of `type`. */
export function eventAttr(msg: RenderMessage, type: string, key: string): string | null {
  for (const e of msg.events ?? []) {
    if (e.type === type && e.attrs[key] != null && e.attrs[key] !== "") {
      return String(e.attrs[key]);
    }
  }
  return null;
}

/** An SDK coin string (`70255ubze,12ibc/ED07…`): the amount is the leading digits, the denom the rest. */
export function parseCoins(text: string | null): Coin[] {
  if (!text) return [];
  return text
    .split(",")
    .map((part) => /^(\d+)(.+)$/.exec(part.trim()))
    .filter((m): m is RegExpExecArray => m !== null)
    .map((m) => ({ amount: m[1], denom: m[2] }));
}

/** `10 BZE`; the raw base amount and denom when the denom is unknown. */
export function coinText(c: Coin, ctx: RenderContext): string {
  const { symbol, exponent } = ctx.denom(c.denom);
  return symbol != null && exponent != null ? formatAmount(c.amount, exponent, symbol) : `${c.amount} ${c.denom}`;
}

/** `10 BZE and 2 USDC`; "nothing" for none. */
export function coinsText(list: Coin[], ctx: RenderContext): string {
  if (list.length === 0) return "nothing";
  const parts = list.map((c) => coinText(c, ctx));
  return parts.length === 1 ? parts[0] : `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;
}

/** `≈ $11.50` when the denom's price is known. */
export function usdText(c: Coin, ctx: RenderContext): string | null {
  const { exponent, priceUsd } = ctx.denom(c.denom);
  if (priceUsd == null || exponent == null) return null;
  const value = new BigNumber(c.amount).shiftedBy(-exponent).times(priceUsd);
  return value.isFinite() ? formatUsd(value.toFixed()) : null;
}

/** An account's label, else its short address. */
export function accountName(address: string | null, ctx: RenderContext): string {
  if (!address) return "unknown";
  return ctx.label(address)?.name ?? shortAddress(address);
}

/** A validator's moniker, else its short operator address. */
export function validatorName(operator: string | null, ctx: RenderContext): string {
  if (!operator) return "a validator";
  return ctx.validator(operator) ?? shortAddress(operator);
}

/** The transfers one message caused. */
export function transfersOf(msg: RenderMessage, ctx: RenderContext): Transfer[] {
  return msg.msg_index === null ? [] : ctx.transfers.filter((t) => t.msg_index === msg.msg_index);
}

/** `MsgCreateDenomReward` → `Create denom reward`, from a type URL or a name. */
export function humanise(typeUrl: string): string {
  const name = typeUrl.split(/[./]/).pop() ?? typeUrl;
  const words = name
    .replace(/^Msg/, "")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .toLowerCase()
    .trim();
  return words ? words[0].toUpperCase() + words.slice(1) : typeUrl;
}

// Body atoms.

export interface Field {
  label: string;
  value: ReactNode;
}

/** Label/value rows; rows whose value is null or undefined are left out. */
export function Fields({ rows }: { rows: (Field | null | false)[] }) {
  const shown = rows.filter((r): r is Field => !!r && r.value != null && r.value !== "");
  return (
    <Grid templateColumns={{ base: "1fr", sm: "minmax(8rem, max-content) 1fr" }} columnGap="4" rowGap={{ base: "1", sm: "2" }} as="dl">
      {shown.map((r) => (
        <Box key={r.label} display="contents">
          <Text as="dt" fontSize="sm" color="fg.muted" mt={{ base: "2", sm: "0" }}>
            {r.label}
          </Text>
          <Box as="dd" fontSize="sm" minW="0" wordBreak="break-word">
            {r.value}
          </Box>
        </Box>
      ))}
    </Grid>
  );
}

/** An account chip with the transaction's label for the address. */
export function Account({ address, ctx }: { address: string | null; ctx: RenderContext }) {
  if (!address) return <Text as="span">unknown</Text>;
  return <AccountChip address={address} label={ctx.label(address)} />;
}

/** A validator chip with its moniker. */
export function Validator({ operator, ctx }: { operator: string | null; ctx: RenderContext }) {
  if (!operator) return <Text as="span">unknown</Text>;
  return <ValidatorChip operator={operator} moniker={ctx.validator(operator)} />;
}

/** An address on another chain: never a link here. */
export function ForeignAddress({ address }: { address: string | null }) {
  return (
    <Text as="span" fontFamily="mono" fontSize="sm" title={address ?? undefined}>
      {address ? shortAddress(address) : "unknown"}
    </Text>
  );
}

/** Amounts in display units, each with its USD value when known. */
export function Coins({ list, ctx }: { list: Coin[]; ctx: RenderContext }) {
  if (list.length === 0) return <Text as="span">nothing</Text>;
  return (
    <Box>
      {list.map((c, i) => {
        const usd = usdText(c, ctx);
        return (
          <Text key={i} fontVariantNumeric="tabular-nums">
            <Text as="span" fontWeight="semibold">
              {coinText(c, ctx)}
            </Text>
            {usd && (
              <Text as="span" color="fg.muted">
                {" "}
                {usd}
              </Text>
            )}
          </Text>
        );
      })}
    </Box>
  );
}

/** `from → to`, stacked on phones. */
export function FromTo({ from, to }: { from: ReactNode; to: ReactNode }) {
  return (
    <HStack gap="2" wrap="wrap">
      {from}
      <Box as="span" color="fg.muted" aria-label="to" display="inline-flex">
        <LuArrowRight />
      </Box>
      {to}
    </HStack>
  );
}
