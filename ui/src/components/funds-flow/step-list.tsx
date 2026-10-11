"use client";

import { Box, Button, Circle, Grid, HStack, Stack, Text } from "@chakra-ui/react";
import { useState } from "react";
import { LuArrowDown, LuArrowRight, LuChevronDown } from "react-icons/lu";

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

function Step({ index, transfer, reason }: { index: number; transfer: Transfer; reason: string }) {
  return (
    <Grid
      as="li"
      templateColumns={{ base: "auto 1fr", md: "auto minmax(0, 1fr) auto auto minmax(0, 1fr)" }}
      columnGap="3"
      rowGap="1.5"
      alignItems="center"
      py="3"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      data-testid="flow-step"
    >
      <Circle size="6" bg="bg.muted" fontSize="xs" fontWeight="semibold" gridRow={{ base: "span 4", md: "auto" }} alignSelf="start">
        {index}
      </Circle>
      <Box minW="0">
        <Party address={transfer.sender} label={transfer.sender_label} none="the chain" />
      </Box>
      <Amount
        amount={transfer.amount}
        denom={transfer.denom}
        symbol={transfer.symbol}
        exponent={transfer.exponent}
        fontWeight="semibold"
        fontSize="sm"
      />
      <Box color="fg.muted" display="inline-flex" aria-hidden>
        <Box display={{ base: "inline-flex", md: "none" }}>
          <LuArrowDown />
        </Box>
        <Box display={{ base: "none", md: "inline-flex" }}>
          <LuArrowRight />
        </Box>
      </Box>
      <Box minW="0">
        <Party address={transfer.recipient} label={transfer.recipient_label} none="burned" />
      </Box>
      <Text fontSize="xs" color="fg.muted" gridColumn={{ base: "2", md: "2 / -1" }}>
        {reason}
      </Text>
    </Grid>
  );
}

function NetBox({ signer, transfers }: { signer: string; transfers: Transfer[] }) {
  const { deltas, fees } = netFor(signer, transfers);
  if (deltas.length === 0 && fees.length === 0) return null;
  const name = partyName(signer, transfers.find((t) => t.sender === signer)?.sender_label ?? null);
  return (
    <Box bg="bg.subtle" rounded="l2" p="4" data-testid="net-box">
      <Text fontSize="sm" fontWeight="semibold" mb="2">
        Net for {name}
      </Text>
      <Stack gap="1" fontSize="sm" fontVariantNumeric="tabular-nums">
        {deltas.map((d) => (
          <Text key={d.denom} color={d.amount.startsWith("-") ? "fg" : "green.fg"}>
            {deltaText(d)}
          </Text>
        ))}
        {fees.map((d) => (
          <Text key={`fee-${d.denom}`} color="fg.muted">
            {deltaText({ ...d, amount: `-${d.amount}` })} fee
          </Text>
        ))}
      </Stack>
    </Box>
  );
}

interface StepListProps {
  transfers: Transfer[];
  /** Why a transfer of an action happened. */
  reasonOf: (t: Transfer) => string;
  /** Start collapsed to the one-line summary. */
  collapsed: boolean;
  /** Whose net to show; no box when null. */
  netOf?: string | null;
}

/** "Where the funds went": one step per transfer in `seq` order. */
export function StepList({ transfers, reasonOf, collapsed, netOf }: StepListProps) {
  const [open, setOpen] = useState(!collapsed);
  const steps = [...transfers].sort((a, b) => a.seq - b.seq);

  if (steps.length === 0) {
    return (
      <Text fontSize="sm" color="fg.muted">
        No funds moved.
      </Text>
    );
  }

  return (
    <Stack gap="3">
      {collapsed && (
        <HStack justify="space-between" gap="3" wrap="wrap">
          <Text fontSize="sm" data-testid="flow-summary">
            {flowSummary(steps)}
          </Text>
          <Button variant="ghost" size="sm" minH="44px" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
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
            <Step key={t.seq} index={i + 1} transfer={t} reason={stepReason(t, reasonOf)} />
          ))}
        </Box>
      )}
      {open && netOf && <NetBox signer={netOf} transfers={steps} />}
    </Stack>
  );
}
