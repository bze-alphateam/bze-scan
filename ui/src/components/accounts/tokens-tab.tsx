"use client";

import { Badge, Box, Button, ButtonGroup, Grid, HStack, Link, Stack, Text } from "@chakra-ui/react";
import { useState } from "react";
import { LuExternalLink } from "react-icons/lu";

import { useDenoms } from "@/api/hooks";
import type { Account } from "@/api/types";
import { Amount } from "@/components/ui/amount";
import { TableSkeleton } from "@/components/ui/skeleton";
import { TokenLogo } from "@/components/tokens/tokens-page";
import { formatCount, formatUsd } from "@/lib/format";
import { sumAmounts } from "@/lib/math";
import { BOND_DENOM, dexAssetUrl, tokenRows, tokensTotalUsd, type TokenRow, type TokenSort } from "@/render/account";
import { bondFull } from "@/render/validators";

const COLUMNS = { base: "minmax(0, 1fr)", md: "minmax(0, 2fr) minmax(0, 1.4fr) minmax(0, 1.6fr) 1fr 120px" };

/** A label shown before a value on phones, where the header row is hidden. */
function PhoneLabel({ children }: { children: string }) {
  return (
    <Text as="span" hideFrom="md" fontSize="xs" color="fg.muted" me="1">
      {children}
    </Text>
  );
}

function RowLogo({ row }: { row: TokenRow }) {
  const denoms = useDenoms();
  return <TokenLogo t={{ denom: row.denom, symbol: row.symbol, logo_url: row.logoUrl, lp_pair: row.lpPair }} denoms={denoms.data} />;
}

function Row({ row, a }: { row: TokenRow; a: Account }) {
  const isBond = row.denom === BOND_DENOM;
  const unbonding = sumAmounts(a.unbonding.map((u) => u.amount));
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
      data-testid="token-row"
    >
      <HStack gap="3" minW="0">
        <RowLogo row={row} />
        <Stack gap="0" minW="0">
          <Text fontWeight="medium" truncate title={row.denom} data-testid="token-symbol">
            {row.symbol ?? row.denom}
          </Text>
          {row.name && (
            <Text fontSize="xs" color="fg.muted" truncate>
              {row.name}
            </Text>
          )}
        </Stack>
      </HStack>
      <Box minW="0">
        <Badge variant="subtle" size="sm" maxW="full" truncate title={row.origin} data-testid="origin-tag">
          {row.origin}
        </Badge>
      </Box>
      <Stack gap="0" fontSize="sm" minW="0">
        <Text wordBreak="break-word">
          <PhoneLabel>Balance</PhoneLabel>
          <Amount amount={row.amount} denom={row.denom} symbol={row.symbol} exponent={row.exponent} />
        </Text>
        {isBond && (
          <Text fontSize="xs" color="fg.muted">
            + {bondFull(a.total_staked ?? "0")} staked · {bondFull(unbonding)} unstaking
          </Text>
        )}
      </Stack>
      <Text fontSize="sm" color={row.valueUsd == null ? "fg.muted" : undefined}>
        <PhoneLabel>Value</PhoneLabel>
        {row.valueUsd == null ? "no price" : formatUsd(row.valueUsd)}
      </Text>
      <Box>
        {row.valueUsd != null && (
          <Link href={dexAssetUrl(row.denom)} target="_blank" rel="noopener noreferrer" fontSize="sm" minH="44px">
            View on DEX <LuExternalLink aria-hidden />
          </Link>
        )}
      </Box>
    </Grid>
  );
}

/** The Tokens tab: every non-zero balance with its origin and value at DEX prices. */
export function TokensTab({ a }: { a: Account }) {
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
  return (
    <Stack gap="3">
      <HStack justify="space-between" wrap="wrap" gap="3">
        <Text fontSize="sm" color="fg.muted" data-testid="tokens-summary">
          {formatCount(rows.length)} {rows.length === 1 ? "token" : "tokens"} · {formatUsd(total)} in total, at DEX prices
        </Text>
        <ButtonGroup size="sm" variant="outline" attached>
          {(["value", "name"] as const).map((s) => (
            <Button
              key={s}
              minH="44px"
              aria-pressed={sort === s}
              variant={sort === s ? "subtle" : "outline"}
              onClick={() => setSort(s)}
            >
              By {s}
            </Button>
          ))}
        </ButtonGroup>
      </HStack>
      <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" overflow="hidden">
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
          <Text>Token</Text>
          <Text>Origin</Text>
          <Text>Balance</Text>
          <Text>Value</Text>
          <span />
        </Grid>
        <Box role="list" aria-label="Tokens">
          {rows.map((row) => (
            <Row key={row.denom} row={row} a={a} />
          ))}
        </Box>
        {rows.length === 0 && (
          <Text px="4" py="6" color="fg.muted" fontSize="sm" borderTopWidth="1px" borderColor="border.panel">
            This account holds no tokens.
          </Text>
        )}
      </Box>
      <Text fontSize="sm" color="fg.muted">
        Tokens without a market on the DEX show no value. Staked and unstaking BZE are counted in the tiles above, not here.
      </Text>
    </Stack>
  );
}
