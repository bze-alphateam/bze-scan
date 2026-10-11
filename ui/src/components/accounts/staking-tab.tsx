"use client";

import { Badge, Box, Grid, HStack, Link, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { LuExternalLink } from "react-icons/lu";

import { useAllValidators } from "@/api/hooks";
import type { Account, Validator } from "@/api/types";
import { Card } from "@/components/ui/card";
import { TableSkeleton } from "@/components/ui/skeleton";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { amountOf, BOND_DENOM, stakingRows, unlockText, validatorStatus, type StakingRow } from "@/render/account";
import { bondFull, unbondingText } from "@/render/validators";

const COLUMNS = { base: "minmax(0, 1fr)", md: "minmax(0, 2.2fr) 1.3fr 1fr 1.2fr 96px" };

function PhoneLabel({ children }: { children: string }) {
  return (
    <Text as="span" hideFrom="md" fontSize="xs" color="fg.muted" me="1">
      {children}
    </Text>
  );
}

function ManageLink({ label = "Manage" }: { label?: string }) {
  return (
    <Link href={externalLinks.staking} target="_blank" rel="noopener noreferrer" fontSize="sm" minH="44px">
      {label} <LuExternalLink aria-hidden />
    </Link>
  );
}

function StatusChips({ v }: { v: Validator | undefined }) {
  const status = validatorStatus(v);
  if (!status) return null;
  return (
    <HStack gap="1" wrap="wrap">
      <Badge colorPalette={status.tone} variant="subtle" size="sm" data-testid="validator-status-chip">
        {status.text}
      </Badge>
      {status.uptime && (
        <Badge colorPalette="orange" variant="subtle" size="sm">
          {status.uptime}
        </Badge>
      )}
    </HStack>
  );
}

function Row({ row, v }: { row: StakingRow; v: Validator | undefined }) {
  return (
    <Grid
      role="listitem"
      templateColumns={COLUMNS}
      gap={{ base: "1.5", md: "4" }}
      alignItems="center"
      px="4"
      py="3"
      minH="56px"
      borderTopWidth="1px"
      borderColor="border.panel"
      data-testid="delegation-row"
    >
      <Stack gap="1" minW="0" align="start">
        <ValidatorChip operator={row.validator} moniker={row.moniker} />
        <StatusChips v={v} />
      </Stack>
      <Text fontSize="sm">
        <PhoneLabel>Staked</PhoneLabel>
        {bondFull(row.amount)}
      </Text>
      <Text fontSize="sm">
        <PhoneLabel>Share of your stake</PhoneLabel>
        {row.share}
      </Text>
      <Text fontSize="sm">
        <PhoneLabel>Rewards to claim</PhoneLabel>
        {row.rewards ? bondFull(row.rewards) : "—"}
      </Text>
      <Box>
        <ManageLink />
      </Box>
    </Grid>
  );
}

function Header() {
  return (
    <Grid
      hideBelow="md"
      templateColumns={COLUMNS}
      gap="4"
      px="4"
      h="10"
      alignItems="center"
      fontSize="xs"
      color="fg.muted"
      textTransform="uppercase"
      letterSpacing="wide"
      aria-hidden
    >
      <Text>Validator</Text>
      <Text>Staked</Text>
      <Text>Share of your stake</Text>
      <Text>Rewards to claim</Text>
      <span />
    </Grid>
  );
}

/** The Staking tab: the delegations with their validators' status, a total, and what is unstaking. */
export function StakingTab({ a }: { a: Account }) {
  const validators = useAllValidators();
  if (!validators.data && !validators.error) return <TableSkeleton rows={Math.max(a.delegations.length, 3)} columns={5} />;
  if (!a.live.available) {
    return (
      <Text fontSize="sm" color="fg.muted">
        The stake will show once the node answers again.
      </Text>
    );
  }
  const byOperator = new Map(validators.data?.items.map((v) => [v.operator_address, v]));
  const rows = stakingRows(a);
  const n = rows.length;
  const rewards = amountOf(a.total_rewards, BOND_DENOM);
  const period = unbondingText(validators.data?.summary?.unbonding_period);
  const now = new Date();
  return (
    <Stack gap="5">
      <HStack justify="space-between" wrap="wrap" gap="3">
        <Text fontSize="sm" color="fg.muted" data-testid="staking-summary">
          {bondFull(a.total_staked ?? "0")} staked with {formatCount(n)} {n === 1 ? "validator" : "validators"} ·{" "}
          {bondFull(rewards)} of rewards waiting
        </Text>
        <ManageLink label="Manage on Staking" />
      </HStack>
      <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" overflow="hidden">
        <Header />
        <Box role="list" aria-label="Stake by validator">
          {rows.map((row) => (
            <Row key={row.validator} row={row} v={byOperator.get(row.validator)} />
          ))}
        </Box>
        {n === 0 ? (
          <Text px="4" py="6" color="fg.muted" fontSize="sm" borderTopWidth="1px" borderColor="border.panel">
            This account stakes with no validator.
          </Text>
        ) : (
          <Grid
            templateColumns={COLUMNS}
            gap={{ base: "1.5", md: "4" }}
            px="4"
            py="3"
            borderTopWidth="1px"
            borderColor="border.panel"
            fontSize="sm"
            fontWeight="semibold"
            data-testid="delegation-total"
          >
            <Text>Total</Text>
            <Text>
              <PhoneLabel>Staked</PhoneLabel>
              {bondFull(a.total_staked ?? "0")}
            </Text>
            <Text>
              <PhoneLabel>Share of your stake</PhoneLabel>
              100%
            </Text>
            <Text>
              <PhoneLabel>Rewards to claim</PhoneLabel>
              {bondFull(rewards)}
            </Text>
          </Grid>
        )}
      </Box>
      {a.unbonding.length > 0 && (
        <Card title={`Unstaking · ${formatCount(a.unbonding.length)}`}>
          <Stack gap="0" role="list" aria-label="Unstaking">
            {a.unbonding.map((u, i) => (
              <Text
                key={`${u.validator}-${u.completion_time}-${i}`}
                role="listitem"
                fontSize="sm"
                py="2"
                borderTopWidth={i === 0 ? "0" : "1px"}
                borderColor="border.panel"
                data-testid="unbonding-item"
              >
                {bondFull(u.amount)} from{" "}
                <Link asChild>
                  <NextLink href={`/validators/${encodeURIComponent(u.validator)}`}>{u.moniker ?? u.validator}</NextLink>
                </Link>{" "}
                <Text as="span" color="fg.muted">
                  · {unlockText(u, now)} · no rewards while unstaking
                </Text>
              </Text>
            ))}
          </Stack>
        </Card>
      )}
      <Text fontSize="sm" color="fg.muted">
        Staked BZE never leaves this account; a validator cannot move it. Unstaking takes {period ?? "a few weeks"}, see{" "}
        <Link asChild>
          <NextLink href="/parameters#staking">the staking parameters</NextLink>
        </Link>
        .
      </Text>
    </Stack>
  );
}
