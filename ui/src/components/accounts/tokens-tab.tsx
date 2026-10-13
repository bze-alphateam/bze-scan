"use client";

import { Box, Flex, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useState } from "react";

import { useDenoms } from "@/api/hooks";
import type { Account } from "@/api/types";
import { TokenLogo } from "@/components/tokens/tokens-page";
import { Amount } from "@/components/ui/amount";
import { Chip } from "@/components/ui/chip";
import { ExplainedBadges } from "@/components/ui/explained-badge";
import { FilterPills } from "@/components/ui/filter-pills";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { TableSkeleton } from "@/components/ui/skeleton";
import { formatCount, formatUsd } from "@/lib/format";
import { sumAmounts } from "@/lib/math";
import { BOND_DENOM, dexAssetUrl, tokenRows, tokensTotalUsd, type TokenRow, type TokenSort } from "@/render/account";
import { tokenBadges } from "@/render/badges";
import { tokenHref } from "@/render/tokens";
import { bondFull } from "@/render/validators";

// Token · Origin (c2) · Balance · Value (c2) · DEX link (c3). On phones the
// origin chips take a second row and the value sits under the balance.
const COLUMNS = {
  base: "minmax(0, 1fr) auto",
  c2: "minmax(0, 1.4fr) minmax(0, 1.3fr) auto auto",
  c3: "minmax(0, 1.4fr) minmax(0, 1.3fr) auto auto 110px",
};
const ORIGIN_COLUMN = { base: "1 / -1", c2: "auto" };
const ORIGIN_ROW = { base: "2", c2: "auto" };

const SORTS: { label: string; value: TokenSort }[] = [
  { label: "By value", value: "value" },
  { label: "By name", value: "name" },
];

function RowLogo({ row }: { row: TokenRow }) {
  const denoms = useDenoms();
  return <TokenLogo t={{ denom: row.denom, symbol: row.symbol, logo_url: row.logoUrl, lp_pair: row.lpPair }} denoms={denoms.data} />;
}

function ValueText({ row }: { row: TokenRow }) {
  return row.valueUsd == null ? (
    <Text as="span" color="fg.muted">
      no price
    </Text>
  ) : (
    <>{formatUsd(row.valueUsd)}</>
  );
}

function Row({ row, staked, unstaking }: { row: TokenRow; staked: string | null; unstaking: string | null }) {
  const name = row.symbol ?? row.denom;
  return (
    <ListRow role="listitem" templateColumns={COLUMNS} rowGap="2" data-testid="token-row">
      <Flex align="center" gap="10px" minW="0">
        <RowLogo row={row} />
        <Box minW="0">
          {row.token ? (
            <Link asChild display="block" fontWeight="500" color="accent.fg" truncate>
              <NextLink href={tokenHref(row.denom)} title={row.denom} data-testid="token-symbol">
                {name}
              </NextLink>
            </Link>
          ) : (
            <Text fontWeight="500" truncate title={row.denom} data-testid="token-symbol">
              {name}
            </Text>
          )}
          {row.name && (
            <Text fontSize="12px" color="fg.muted" truncate>
              {row.name}
            </Text>
          )}
        </Box>
      </Flex>
      <Box gridColumn={ORIGIN_COLUMN} gridRow={ORIGIN_ROW} minW="0">
        {row.token ? (
          <ExplainedBadges badges={tokenBadges(row.token)} />
        ) : (
          <Chip whiteSpace="normal" data-testid="origin-tag">
            {row.origin}
          </Chip>
        )}
      </Box>
      <Box textAlign="right" minW="0">
        <Text fontFamily="mono" fontSize="13px" wordBreak="break-word">
          <Amount amount={row.amount} denom={row.denom} symbol={row.symbol} exponent={row.exponent} />
        </Text>
        {row.denom === BOND_DENOM && staked != null && (
          <Text fontSize="12px" color="fg.muted">
            + {bondFull(staked)} staked · {bondFull(unstaking ?? "0")} unstaking
          </Text>
        )}
        <Text hideFrom="c2" fontFamily="mono" fontSize="12px" color="fg.muted">
          <ValueText row={row} />
        </Text>
      </Box>
      <Text hideBelow="c2" textAlign="right" fontFamily="mono" fontSize="13px" whiteSpace="nowrap">
        <ValueText row={row} />
      </Text>
      <Box hideBelow="c3" textAlign="right">
        {row.valueUsd != null && (
          <Link href={dexAssetUrl(row.denom)} target="_blank" rel="noopener noreferrer" fontSize="13px" color="accent.fg" whiteSpace="nowrap">
            View on DEX ↗
          </Link>
        )}
      </Box>
    </ListRow>
  );
}

/**
 * Every non-zero balance with its origin and value at DEX prices: a personal
 * account's Tokens tab, a module account's Holdings (`module`, where staking
 * has no place).
 */
export function TokensTab({ a, module = false }: { a: Account; module?: boolean }) {
  const [sort, setSort] = useState<TokenSort>("value");
  const denoms = useDenoms();
  // Without the tokens list the rows still render, unpriced, once it failed.
  if (!denoms.data && !denoms.error) return <TableSkeleton rows={5} columns={5} />;
  if (!a.live.available) {
    return (
      <Text fontSize="sm" color="fg.muted">
        The balances will show once the node answers again.
      </Text>
    );
  }
  const rows = tokenRows(a.balances, denoms.data, sort);
  const total = tokensTotalUsd(rows);
  const staked = module ? null : (a.total_staked ?? "0");
  const unstaking = module ? null : sumAmounts(a.unbonding.map((u) => u.amount));
  return (
    <Box>
      <Flex justify="space-between" align="center" gap="3" wrap="wrap" mb="14px" fontSize="13px">
        <Text color="fg.muted" data-testid="tokens-summary">
          {formatCount(rows.length)} {rows.length === 1 ? "token" : "tokens"} ·{" "}
          <Text as="span" fontFamily="mono">
            {formatUsd(total)}
          </Text>{" "}
          in total, at DEX prices
        </Text>
        <FilterPills label="Sort the tokens" options={SORTS} value={sort} onChange={setSort} mb="0" />
      </Flex>
      <ListPanel>
        <ListHead templateColumns={COLUMNS} aria-hidden>
          <Text>Token</Text>
          <Text hideBelow="c2">Origin</Text>
          <Text textAlign="right">Balance</Text>
          <Text hideBelow="c2" textAlign="right">
            Value
          </Text>
          <Text hideBelow="c3" />
        </ListHead>
        <Box role="list" aria-label="Tokens">
          {rows.map((row) => (
            <Row key={row.denom} row={row} staked={staked} unstaking={unstaking} />
          ))}
        </Box>
        {rows.length === 0 && (
          <Text px="4" py="6" color="fg.muted" fontSize="sm">
            This account holds no tokens.
          </Text>
        )}
      </ListPanel>
      <Text mt="3" fontSize="13px" color="fg.muted">
        Tokens without a market on the DEX show no value.
        {!module && " Staked and unstaking BZE are counted in the tiles above, not here."}
      </Text>
    </Box>
  );
}
