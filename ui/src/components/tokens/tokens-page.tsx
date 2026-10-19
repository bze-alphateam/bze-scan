"use client";

import { Box, Button, Flex, Link, SimpleGrid, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useMemo, useState } from "react";
import { LuTriangleAlert } from "react-icons/lu";

import { AppLink } from "@/components/layout/app-link";
import { useAllTokens } from "@/api/hooks";
import type { LpPair, TokenSummary } from "@/api/types";
import { Avatar } from "@/components/ui/avatar";
import { ExplainedBadges } from "@/components/ui/explained-badge";
import { FilterPills } from "@/components/ui/filter-pills";
import { FindBox } from "@/components/ui/find-box";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { PageTitle } from "@/components/ui/page-head";
import { TableSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { Tile } from "@/components/ui/tile";
import { formatCount } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { BOND_DENOM, dexAssetUrl } from "@/render/account";
import { tokenBadges } from "@/render/badges";
import {
  changeText,
  filterTokens,
  noPriceText,
  priceText,
  supplyText,
  TOKEN_FILTERS,
  tokenHref,
  tokensTiles,
  tokenSymbol,
  type TokenFilter,
} from "@/render/tokens";

// Token · Origin (c2; under the token on phones) · Supply (c2) · Holders (c3) · Price · 24h · DEX (c3).
// (Custom breakpoints are not in Chakra's generated types, so these objects are hoisted.)
// Every row is its own grid, so from c2 no column may be `auto`: it would size
// to each row's content and the columns would not line up.
const COLUMNS = {
  base: "minmax(0, 1fr) auto",
  c2: "minmax(0, 1fr) minmax(0, 1.6fr) minmax(0, 1fr) 150px",
  c3: "minmax(0, 1fr) minmax(150px, 1.8fr) minmax(0, 1fr) 80px 150px 110px",
};
// On phones the origin chips take a row of their own under the token and its price.
const ORIGIN_COLUMN = { base: "1 / -1", c2: "auto" };
const ORIGIN_ROW = { base: "2", c2: "auto" };
const TILE_COLUMNS = { base: 1, sm: 2, lg: 4 };

type LogoToken = Pick<TokenSummary, "logo_url" | "symbol" | "denom"> & { lp_pair?: LpPair | null };

/**
 * A token's logo, else its avatar; a pool share shows its two assets' logos,
 * one over the other's corner, looked up in `denoms` (the caller's tokens).
 */
export function TokenLogo({ t, denoms, size = "30px" }: { t: LogoToken; denoms?: Map<string, TokenSummary>; size?: string }) {
  if (t.lp_pair) {
    const side = (denom: string) => denoms?.get(denom) ?? { denom, symbol: null, logo_url: null };
    return (
      <Box position="relative" boxSize={size} flexShrink={0} data-testid="lp-logo">
        <Box position="absolute" top="0" left="0" w="70%" h="70%">
          <SingleLogo t={side(t.lp_pair.base)} size="full" />
        </Box>
        <Box position="absolute" bottom="0" right="0" w="70%" h="70%" rounded="full" outline="2px solid" outlineColor="bg">
          <SingleLogo t={side(t.lp_pair.quote)} size="full" />
        </Box>
      </Box>
    );
  }
  return <SingleLogo t={t} size={size} />;
}

/** What a token without a logo shows: the DEX's placeholder. */
export const TOKEN_LOGO_PLACEHOLDER = "/images/token.svg";

/** BZE's own logo, the one the DEX shows, over the registry's. */
export const BZE_LOGO = "/images/bze_alternative_512x512.png";

function SingleLogo({ t, size }: { t: Pick<TokenSummary, "logo_url" | "symbol" | "denom">; size: string }) {
  return (
    <Avatar
      name={tokenSymbol(t)}
      src={t.denom === BOND_DENOM ? BZE_LOGO : t.logo_url}
      fallbackSrc={TOKEN_LOGO_PLACEHOLDER}
      variant="token"
      size={size}
      fontSize={size === "full" ? "2xs" : undefined}
    />
  );
}

function Tiles({ tokens }: { tokens: TokenSummary[] }) {
  const tiles = tokensTiles(tokens, new Date());
  return (
    <SimpleGrid columns={TILE_COLUMNS} gap="3" mb="7" data-testid="tokens-tiles">
      <Tile label="Tokens on BZE" value={formatCount(tiles.count)} sub={tiles.byKind} />
      <Tile
        label="On the DEX"
        value={formatCount(tiles.onDex)}
        sub={
          <>
            with a price on{" "}
            <AppLink app="dex" href={externalLinks.dex}>
              DEX
            </AppLink>
          </>
        }
      />
      <Tile
        label="Created this month"
        value={formatCount(tiles.createdThisMonth)}
        sub={
          <>
            on{" "}
            <AppLink app="factory" href={externalLinks.factory}>
              Factory
            </AppLink>
          </>
        }
      />
      <Tile
        label="Cross-chain"
        value={`${formatCount(tiles.chains.length)} ${tiles.chains.length === 1 ? "chain" : "chains"}`}
        sub={tiles.chains.length > 0 ? tiles.chains.join(", ") : "no tokens from other chains yet"}
      />
    </SimpleGrid>
  );
}

function PriceCell({ t }: { t: TokenSummary }) {
  const none = noPriceText(t);
  if (none) {
    return (
      <Text textAlign="right" fontSize="13px" color={t.halted ? "neg.fg" : "fg.muted"} whiteSpace="nowrap">
        {none}
      </Text>
    );
  }
  const change = changeText(t.price_change_24h_pct);
  const tone = change ? { green: "pos.fg", red: "neg.fg", gray: "fg.muted" }[change.tone] : undefined;
  return (
    <Text textAlign="right" fontFamily="mono" fontSize="13px" whiteSpace="nowrap">
      {priceText(t.price_usd!)}
      {change && (
        <Text as="span" color={tone} ms="6px">
          {change.text}
        </Text>
      )}
    </Text>
  );
}

function TokenRow({ t, denoms }: { t: TokenSummary; denoms: Map<string, TokenSummary> }) {
  return (
    <ListRow role="listitem" templateColumns={COLUMNS} rowGap="2" data-testid="token-list-row">
      <Flex align="center" gap="10px" minW="0">
        <TokenLogo t={t} denoms={denoms} />
        <Box minW="0">
          <Link asChild display="block" fontWeight="500" color="accent.fg" truncate>
            <NextLink href={tokenHref(t.denom)} data-testid="token-link">
              {tokenSymbol(t)}
            </NextLink>
          </Link>
          {t.name && (
            <Text fontSize="12px" color="fg.muted" truncate>
              {t.name}
            </Text>
          )}
        </Box>
      </Flex>
      <Box gridColumn={ORIGIN_COLUMN} gridRow={ORIGIN_ROW} minW="0">
        <ExplainedBadges badges={tokenBadges(t)} />
      </Box>
      <Text hideBelow="c2" textAlign="right" fontFamily="mono" fontSize="13px" wordBreak="break-word">
        {supplyText(t) ?? "—"}
      </Text>
      <Text hideBelow="c3" textAlign="right" fontFamily="mono" fontSize="13px" color={t.holders_count ? undefined : "fg.muted"}>
        {t.holders_count != null ? formatCount(t.holders_count) : "—"}
      </Text>
      <PriceCell t={t} />
      <Box hideBelow="c3" textAlign="right">
        {t.price_usd != null && (
          <AppLink app="dex" href={dexAssetUrl(t.denom)}>
            DEX
          </AppLink>
        )}
      </Box>
    </ListRow>
  );
}

function TokenTable({ rows, denoms, empty }: { rows: TokenSummary[]; denoms: Map<string, TokenSummary>; empty: string }) {
  return (
    <ListPanel>
      <ListHead templateColumns={COLUMNS} aria-hidden>
        <Text>Token</Text>
        <Text hideBelow="c2">Origin</Text>
        <Text hideBelow="c2" textAlign="right">
          Supply on BZE
        </Text>
        <Text hideBelow="c3" textAlign="right">
          Holders
        </Text>
        <Text textAlign="right">Price · 24h</Text>
        <Box hideBelow="c3" />
      </ListHead>
      <Box role="list" aria-label="Tokens">
        {rows.map((t) => (
          <TokenRow key={t.denom} t={t} denoms={denoms} />
        ))}
      </Box>
      {rows.length === 0 && (
        <Text px="4" py="6" color="fg.muted" fontSize="13px">
          {empty}
        </Text>
      )}
    </ListPanel>
  );
}

/** The list once loaded: tiles, the kind pills with the search box, the table and the footnote. */
export function TokensView({ tokens }: { tokens: TokenSummary[] }) {
  const [filter, setFilter] = useState<TokenFilter>("all");
  const [query, setQuery] = useState("");
  const rows = filterTokens(tokens, filter, query);
  const denoms = useMemo(() => new Map(tokens.map((t) => [t.denom, t])), [tokens]);
  return (
    <Box>
      <Tiles tokens={tokens} />
      <Flex justify="space-between" align="center" columnGap="4" rowGap="3" wrap="wrap" mb="14px">
        <FilterPills label="Filter by origin" options={TOKEN_FILTERS} value={filter} onChange={setFilter} mb="0" />
        <FindBox label="Find a token" value={query} onChange={setQuery} />
      </Flex>
      <TokenTable rows={rows} denoms={denoms} empty={query ? "No token has that name." : "No tokens of this kind yet."} />
      <Text mt="3" fontSize="12px" color="fg.muted">
        Raw names (factory/…, ibc/…) are in each token&apos;s More details.
      </Text>
    </Box>
  );
}

export function TokensSkeleton() {
  return (
    <Stack gap="7" aria-busy data-testid="tokens-skeleton">
      <SimpleGrid columns={TILE_COLUMNS} gap="3">
        {Array.from({ length: 4 }).map((_, i) => (
          <TileSkeleton key={i} />
        ))}
      </SimpleGrid>
      <TableSkeleton rows={10} columns={6} />
    </Stack>
  );
}

/** `/tokens`: the skeleton on the first load, then the list (polled; never a skeleton again). */
export function TokensPage() {
  const { data, error, refetch } = useAllTokens();
  return (
    <Box>
      <PageTitle title="Tokens" intro="Every token on BZE. Prices come from the BZE DEX." />
      {data ? (
        <TokensView tokens={data} />
      ) : error ? (
        <StateBox
          tone="error"
          icon={<LuTriangleAlert size={20} />}
          title="The tokens could not be loaded"
          actions={
            <Button size="sm" colorPalette="blue" onClick={() => void refetch()}>
              Try again
            </Button>
          }
          data-testid="tokens-error"
        >
          The explorer did not answer. Try again in a moment.
        </StateBox>
      ) : (
        <TokensSkeleton />
      )}
    </Box>
  );
}
