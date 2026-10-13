"use client";

import { Box, Button, Grid, HStack, Stack, Text } from "@chakra-ui/react";
import { useState, type ReactNode } from "react";
import { LuChevronDown } from "react-icons/lu";

import type { Transfer } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Amount } from "@/components/ui/amount";
import { formatAmount, shortAddress } from "@/lib/format";

import { deltaText, netFor } from "./net";

/** The reason of a transfer: the fee, a mint, a burn, else what the action says. */
export function stepReason(t: Transfer, actionReason: (t: Transfer) => string): string {
  if (t.msg_index === null) return "transaction fee";
  if (t.kind === "mint") return "created by the chain";
  if (t.kind === "burn") return "burned";
  return actionReason(t);
}

const amountText = (t: Transfer) =>
  t.symbol != null && t.exponent != null ? formatAmount(t.amount, t.exponent, t.symbol) : `${t.amount} ${t.denom}`;

const partyName = (address: string | null, label: Transfer["sender_label"]) =>
  label?.name ?? (address ? shortAddress(address) : "");

/** "2 transfers · 10 BZE to bze1m3k…9ds4 · 0.002 BZE fee to Fee collector". */
export function flowSummary(transfers: Transfer[]): string {
  const parts = transfers.map((t) => {
    const amount = amountText(t);
    if (t.kind === "burn") return `${amount} burned`;
    const to = partyName(t.recipient, t.recipient_label);
    if (t.kind === "mint") return `${amount} created for ${to}`;
    return t.msg_index === null ? `${amount} fee to ${to}` : `${amount} to ${to}`;
  });
  const count = `${transfers.length} transfer${transfers.length === 1 ? "" : "s"}`;
  return [count, ...parts].join(" · ");
}

function Party({ address, label, none }: { address: string | null; label: Transfer["sender_label"]; none: string }) {
  if (!address) {
    return (
      <Text as="span" fontSize="sm" color="fg.muted">
        {none}
      </Text>
    );
  }
  return <AccountChip address={address} label={label} />;
}

// n · from · amount · → · to from c2 up; on phones the amount and the recipient stack under the sender.
// (Custom breakpoints are not in Chakra's generated types, so these objects are hoisted.)
const STEP_COLUMNS = { base: "24px minmax(0, 1fr)", c2: "24px minmax(0, 1fr) auto 20px minmax(0, 1fr)" };
const AMOUNT_ALIGN = { base: "left", c2: "right" } as const;
const ON_PHONE_COL2 = { base: "2", c2: "auto" };

function Step({ index, transfer, reason }: { index: number; transfer: Transfer; reason: ReactNode }) {
  return (
    <Grid
      as="li"
      templateColumns={STEP_COLUMNS}
      columnGap="3"
      rowGap="1"
      alignItems="center"
      px="4"
      py="10px"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      data-testid="flow-step"
    >
      <Text as="span" fontFamily="mono" fontSize="12px" color="fg.muted">
        {index}
      </Text>
      <Box minW="0">
        <Party address={transfer.sender} label={transfer.sender_label} none="the chain" />
      </Box>
      <Amount
        amount={transfer.amount}
        denom={transfer.denom}
        symbol={transfer.symbol}
        exponent={transfer.exponent}
        fontFamily="mono"
        fontWeight="500"
        fontSize="13px"
        textAlign={AMOUNT_ALIGN}
        gridColumn={ON_PHONE_COL2}
      />
      <Box hideBelow="c2" color="fg.muted" textAlign="center" aria-hidden>
        →
      </Box>
      <Box minW="0" gridColumn={ON_PHONE_COL2}>
        <Party address={transfer.recipient} label={transfer.recipient_label} none="burned" />
      </Box>
      <Box fontSize="12px" color="fg.muted" gridColumn="2 / -1">
        {reason}
      </Box>
    </Grid>
  );
}

function NetBox({ signer, transfers }: { signer: string; transfers: Transfer[] }) {
  const { deltas, fees } = netFor(signer, transfers);
  if (deltas.length === 0 && fees.length === 0) return null;
  const name = partyName(signer, transfers.find((t) => t.sender === signer)?.sender_label ?? null);
  return (
    <Stack
      gap="1"
      mx="4"
      mt="2"
      mb="4"
      px="14px"
      py="3"
      borderWidth="1px"
      borderColor="border.panel"
      rounded="l2"
      bg="bg.page"
      fontSize="13px"
      data-testid="net-box"
    >
      <Text fontSize="12px" fontWeight="600" mb="2px">
        Net for{" "}
        <Text as="span" fontFamily={name.startsWith("bze1") ? "mono" : undefined}>
          {name}
        </Text>
      </Text>
      {deltas.map((d) => (
        <Text key={d.denom} fontFamily="mono" color={d.amount.startsWith("-") ? "neg.fg" : "pos.fg"}>
          {deltaText(d)}
        </Text>
      ))}
      {fees.map((d) => (
        <Text key={`fee-${d.denom}`} fontFamily="mono" color="fg.muted">
          {deltaText({ ...d, amount: `-${d.amount}` })} fee
        </Text>
      ))}
    </Stack>
  );
}

interface StepListProps<T extends Transfer> {
  transfers: T[];
  /** Why a transfer of an action happened. */
  reasonOf?: (t: T) => string;
  /** The line under a step, in place of its reason (the block page's "in 3F9A…C21E"). */
  describe?: (t: T) => ReactNode;
  /** Start collapsed to the one-line summary. */
  collapsed: boolean;
  /** Whose net to show; no box when null. */
  netOf?: string | null;
  /** A step's key: its `seq` by default, unique within one transaction. */
  keyOf?: (t: T) => string | number;
  /** Keep the given order instead of sorting by `seq` (steps of several transactions). */
  inOrder?: boolean;
}

/** "Where the funds went": one step per transfer, in `seq` order unless `inOrder`. */
export function StepList<T extends Transfer>({
  transfers,
  reasonOf,
  describe,
  collapsed,
  netOf,
  keyOf = (t) => t.seq,
  inOrder = false,
}: StepListProps<T>) {
  const [open, setOpen] = useState(!collapsed);
  const steps = inOrder ? transfers : [...transfers].sort((a, b) => a.seq - b.seq);
  const lineOf = (t: T): ReactNode => (describe ? describe(t) : stepReason(t, (x) => reasonOf?.(x as T) ?? ""));

  if (steps.length === 0) {
    return (
      <Text px="4" py="3" fontSize="13px" color="fg.muted">
        No funds moved.
      </Text>
    );
  }

  return (
    <Box>
      {collapsed && (
        <HStack justify="space-between" gap="3" wrap="wrap" px="4" py="1" borderBottomWidth={open ? "1px" : "0"} borderColor="border.panel">
          <Text fontSize="13px" data-testid="flow-summary">
            {flowSummary(steps)}
          </Text>
          <Button variant="plain" size="sm" minH="44px" px="0" color="accent.fg" fontSize="13px" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
            {open ? "Hide steps" : "Show steps"}
            <Box as="span" transform={open ? "rotate(180deg)" : undefined} display="inline-flex">
              <LuChevronDown />
            </Box>
          </Button>
        </HStack>
      )}
      {open && (
        <Box as="ol" listStyleType="none" m="0" p="0">
          {steps.map((t, i) => (
            <Step key={keyOf(t)} index={i + 1} transfer={t} reason={lineOf(t)} />
          ))}
        </Box>
      )}
      {open && netOf && <NetBox signer={netOf} transfers={steps} />}
    </Box>
  );
}
