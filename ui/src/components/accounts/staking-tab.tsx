"use client";

import { Box, Flex, Link, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";

import { useAllValidators } from "@/api/hooks";
import type { Account, Validator } from "@/api/types";
import { Chip } from "@/components/ui/chip";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { PanelHead } from "@/components/ui/page-head";
import { TableSkeleton } from "@/components/ui/skeleton";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { amountOf, BOND_DENOM, stakingRows, unlockText, validatorStatus, type StakingRow } from "@/render/account";
import { bondFull, unbondingText } from "@/render/validators";

// Validator · Staked · Share of your stake (c2) · Rewards to claim (c3).
const COLUMNS = {
  base: "minmax(0, 1fr) auto",
  c2: "minmax(0, 2fr) minmax(0, 1fr) minmax(0, 1fr)",
  c3: "minmax(0, 2fr) minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1fr)",
};

const TONE = { green: "ok", gray: "default", red: "bad" } as const;

function StatusChips({ v }: { v: Validator | undefined }) {
  const status = validatorStatus(v);
  if (!status) return null;
  return (
    <Flex gap="1" wrap="wrap">
      <Chip variant={TONE[status.tone]} data-testid="validator-status-chip">
        {status.text}
      </Chip>
      {status.uptime && <Chip variant="warn">{status.uptime}</Chip>}
    </Flex>
  );
}

/** A label shown before a value on phones, where the head row's columns are hidden. */
function PhoneLabel({ children }: { children: string }) {
  return (
    <Text as="span" hideFrom="c2" fontFamily="body" fontSize="12px" color="fg.muted" me="1">
      {children}
    </Text>
  );
}

function Row({ row, v }: { row: StakingRow; v: Validator | undefined }) {
  return (
    <ListRow role="listitem" templateColumns={COLUMNS} data-testid="delegation-row">
      <Stack gap="1" minW="0" align="start">
        <ValidatorChip operator={row.validator} moniker={row.moniker} />
        <StatusChips v={v} />
      </Stack>
      <Box textAlign="right" fontFamily="mono" fontSize="13px">
        <Text>{bondFull(row.amount)}</Text>
        <Text hideFrom="c2" fontSize="12px" color="fg.muted">
          <PhoneLabel>Share</PhoneLabel>
          {row.share}
        </Text>
        <Text hideFrom="c3" fontSize="12px" color="fg.muted">
          <PhoneLabel>Rewards</PhoneLabel>
          {row.rewards ? bondFull(row.rewards) : "—"}
        </Text>
      </Box>
      <Text hideBelow="c2" textAlign="right" fontFamily="mono" fontSize="13px">
        {row.share}
      </Text>
      <Text hideBelow="c3" textAlign="right" fontFamily="mono" fontSize="13px">
        {row.rewards ? bondFull(row.rewards) : "—"}
      </Text>
    </ListRow>
  );
}

/** The Staking tab: the delegations with their validators' status, a total, and what is unstaking. */
export function StakingTab({ a }: { a: Account }) {
  const validators = useAllValidators();
  if (!validators.data && !validators.error) return <TableSkeleton rows={Math.max(a.delegations.length, 3)} columns={4} />;
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
    <Box>
      <Flex justify="space-between" align="center" gap="3" wrap="wrap" mb="14px" fontSize="13px">
        <Text color="fg.muted" data-testid="staking-summary">
          {bondFull(a.total_staked ?? "0")} staked with {formatCount(n)} {n === 1 ? "validator" : "validators"} ·{" "}
          {bondFull(rewards)} of rewards waiting
        </Text>
        <Link
          href={externalLinks.staking}
          target="_blank"
          rel="noopener noreferrer"
          display="inline-flex"
          alignItems="center"
          h="30px"
          px="3"
          borderWidth="1px"
          borderColor="border.panel"
          rounded="l2"
          fontSize="13px"
          color="fg"
          _hover={{ borderColor: "accent.fg", color: "accent.fg", textDecoration: "none" }}
        >
          Manage on Staking ↗
        </Link>
      </Flex>
      <ListPanel>
        <ListHead templateColumns={COLUMNS} aria-hidden>
          <Text>Validator</Text>
          <Text textAlign="right">Staked</Text>
          <Text hideBelow="c2" textAlign="right">
            Share of your stake
          </Text>
          <Text hideBelow="c3" textAlign="right">
            Rewards to claim
          </Text>
        </ListHead>
        <Box role="list" aria-label="Stake by validator">
          {rows.map((row) => (
            <Row key={row.validator} row={row} v={byOperator.get(row.validator)} />
          ))}
        </Box>
        {n === 0 ? (
          <Text px="4" py="22px" color="fg.muted" fontSize="13px" textAlign="center">
            This account stakes with no validator.
          </Text>
        ) : (
          <ListRow
            templateColumns={COLUMNS}
            borderTopWidth="1px"
            fontSize="13px"
            fontWeight="600"
            _hover={undefined}
            data-testid="delegation-total"
          >
            <Text>Total</Text>
            <Text textAlign="right" fontFamily="mono">
              {bondFull(a.total_staked ?? "0")}
            </Text>
            <Text hideBelow="c2" textAlign="right" fontFamily="mono">
              100%
            </Text>
            <Text hideBelow="c3" textAlign="right" fontFamily="mono">
              {bondFull(rewards)}
            </Text>
          </ListRow>
        )}
      </ListPanel>
      {a.unbonding.length > 0 && (
        <ListPanel mt="5">
          <PanelHead id="unstaking-head" title={`Unstaking · ${formatCount(a.unbonding.length)}`} />
          <Box role="list" aria-labelledby="unstaking-head">
            {a.unbonding.map((u, i) => (
              <Text
                key={`${u.validator}-${u.completion_time}-${i}`}
                role="listitem"
                fontSize="13px"
                px="4"
                py="10px"
                borderTopWidth={i === 0 ? "0" : "1px"}
                borderColor="border.panel"
                data-testid="unbonding-item"
              >
                <Text as="span" fontFamily="mono">
                  {bondFull(u.amount)}
                </Text>{" "}
                from{" "}
                <Link asChild color="accent.fg">
                  <NextLink href={`/validators/${encodeURIComponent(u.validator)}`}>{u.moniker ?? u.validator}</NextLink>
                </Link>{" "}
                <Text as="span" color="fg.muted">
                  · {unlockText(u, now)} · no rewards while unstaking
                </Text>
              </Text>
            ))}
          </Box>
        </ListPanel>
      )}
      <Text mt="3" fontSize="13px" color="fg.muted">
        Staked BZE never leaves this account; a validator cannot move it. Unstaking takes {period ?? "a few weeks"}, see{" "}
        <Link asChild color="accent.fg">
          <NextLink href="/parameters#staking">the staking parameters</NextLink>
        </Link>
        .
      </Text>
    </Box>
  );
}
