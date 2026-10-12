"use client";

import { Badge, Box, Button, Grid, Heading, HStack, Image, Input, Link, SimpleGrid, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useMemo, useState } from "react";
import { LuExternalLink, LuSearch } from "react-icons/lu";

import { useAllTokens } from "@/api/hooks";
import type { LpPair, TokenSummary } from "@/api/types";
import { Avatar } from "@/components/ui/avatar";
import { Card } from "@/components/ui/card";
import { TableSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { Tile } from "@/components/ui/tile";
import { formatCount } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { dexAssetUrl, multiHop } from "@/render/account";
import {
  changeText,
  filterTokens,
  hopsExplainer,
  listOriginTag,
  noPriceText,
  priceText,
  supplyText,
  TOKEN_FILTERS,
  tokenHref,
  tokensTiles,
  tokenSymbol,
  type TokenFilter,
} from "@/render/tokens";

const COLUMNS = {
  base: "minmax(0, 1fr)",
  md: "minmax(0, 2fr) minmax(0, 1.4fr) minmax(0, 1.2fr) minmax(0, 0.8fr) minmax(0, 1.3fr) 120px",
};

/** A label shown before a value on phones, where the header row is hidden. */
function PhoneLabel({ children }: { children: string }) {
  return (
    <Text as="span" hideFrom="md" fontSize="xs" color="fg.muted" me="1">
      {children}
    </Text>
  );
}

type LogoToken = Pick<TokenSummary, "logo_url" | "symbol" | "denom"> & { lp_pair?: LpPair | null };

/**
 * A token's logo, else its avatar; a pool share shows its two assets' logos,
 * one over the other's corner, looked up in `denoms` (the caller's tokens).
 */
export function TokenLogo({ t, denoms, size = "9" }: { t: LogoToken; denoms?: Map<string, TokenSummary>; size?: string }) {
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

function SingleLogo({ t, size }: { t: Pick<TokenSummary, "logo_url" | "symbol" | "denom">; size: string }) {
  if (t.logo_url) return <Image src={t.logo_url} alt="" boxSize={size} rounded="full" flexShrink={0} />;
  return <Avatar name={tokenSymbol(t)} size={size} fontSize={size === "full" ? "2xs" : undefined} />;
}

function Tiles({ tokens }: { tokens: TokenSummary[] }) {
  const tiles = tokensTiles(tokens, new Date());
  return (
    <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3" data-testid="tokens-tiles">
      <Tile label="Tokens on BZE" value={formatCount(tiles.count)} sub={tiles.byKind} />
      <Tile
        label="On the DEX"
        value={formatCount(tiles.onDex)}
        sub={
          <Link href={externalLinks.dex} target="_blank" rel="noopener noreferrer">
            with a price on the DEX <LuExternalLink aria-hidden />
          </Link>
        }
      />
      <Tile
        label="Created this month"
        value={formatCount(tiles.createdThisMonth)}
        sub={
          <Link href={externalLinks.factory} target="_blank" rel="noopener noreferrer">
            on Factory <LuExternalLink aria-hidden />
          </Link>
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
      <Text fontSize="sm" color={t.halted ? "red.fg" : "fg.muted"}>
        <PhoneLabel>Price</PhoneLabel>
        {none}
      </Text>
    );
  }
  const change = changeText(t.price_change_24h_pct);
  return (
    <Text fontSize="sm">
      <PhoneLabel>Price</PhoneLabel>
      {priceText(t.price_usd!)}
      {change && (
        <Text as="span" color={`${change.tone}.fg`}>
          {" "}
          {change.text}
        </Text>
      )}
    </Text>
  );
}

function TokenRow({ t, denoms }: { t: TokenSummary; denoms: Map<string, TokenSummary> }) {
  const hop = multiHop(t);
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
      data-testid="token-list-row"
    >
      <HStack gap="3" minW="0">
        <TokenLogo t={t} denoms={denoms} />
        <Stack gap="0" minW="0">
          <Link asChild fontWeight="medium" truncate>
            <NextLink href={tokenHref(t.denom)} data-testid="token-link">
              {tokenSymbol(t)}
            </NextLink>
          </Link>
          {t.name && (
            <Text fontSize="xs" color="fg.muted" truncate>
              {t.name}
            </Text>
          )}
        </Stack>
      </HStack>
      <HStack gap="1" minW="0" wrap="wrap">
        <Badge variant="subtle" size="sm" maxW="full" h="auto" py="0.5" whiteSpace="normal" data-testid="origin-tag">
          {listOriginTag(t)}
        </Badge>
        {hop && (
          <Badge colorPalette="orange" variant="subtle" size="sm" title={hopsExplainer(t) ?? undefined} data-testid="hops-tag">
            {hop.hops} hops
          </Badge>
        )}
      </HStack>
      <Text fontSize="sm" wordBreak="break-word">
        <PhoneLabel>Supply</PhoneLabel>
        {supplyText(t) ?? "—"}
      </Text>
      <Text fontSize="sm">
        <PhoneLabel>Holders</PhoneLabel>
        {t.holders_count != null ? formatCount(t.holders_count) : "—"}
      </Text>
      <PriceCell t={t} />
      <Box>
        {t.price_usd != null && (
          <Link href={dexAssetUrl(t.denom)} target="_blank" rel="noopener noreferrer" fontSize="sm" minH="44px">
            View on DEX <LuExternalLink aria-hidden />
          </Link>
        )}
      </Box>
    </Grid>
  );
}

function TokenTable({ rows, denoms, empty }: { rows: TokenSummary[]; denoms: Map<string, TokenSummary>; empty: string }) {
  return (
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
        <Text>Supply on BZE</Text>
        <Text>Holders</Text>
        <Text>Price · 24h</Text>
        <span />
      </Grid>
      <Box role="list" aria-label="Tokens">
        {rows.map((t) => (
          <TokenRow key={t.denom} t={t} denoms={denoms} />
        ))}
      </Box>
      {rows.length === 0 && (
        <Text px="4" py="6" color="fg.muted" fontSize="sm" borderTopWidth="1px" borderColor="border.panel">
          {empty}
        </Text>
      )}
    </Box>
  );
}

/** The list once loaded: intro, tiles, chips with the filter, the table and the footnote. */
export function TokensView({ tokens }: { tokens: TokenSummary[] }) {
  const [filter, setFilter] = useState<TokenFilter>("all");
  const [query, setQuery] = useState("");
  const rows = filterTokens(tokens, filter, query);
  const denoms = useMemo(() => new Map(tokens.map((t) => [t.denom, t])), [tokens]);
  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Text color="fg.muted" maxW="3xl">
        Every token on BZE. Prices come from the BZE DEX.
      </Text>
      <Tiles tokens={tokens} />
      <Stack gap="3">
        <HStack justify="space-between" wrap="wrap" gap="3">
          <HStack gap="2" wrap="wrap" role="group" aria-label="Show tokens">
            {TOKEN_FILTERS.map((f) => (
              <Button
                key={f.value}
                size="sm"
                minH="44px"
                rounded="full"
                variant={filter === f.value ? "subtle" : "outline"}
                aria-pressed={filter === f.value}
                onClick={() => setFilter(f.value)}
              >
                {f.label}
              </Button>
            ))}
          </HStack>
          <HStack
            gap="2"
            px="3"
            borderWidth="1px"
            borderColor="border.panel"
            rounded="l2"
            w={{ base: "full", sm: "64" }}
            color="fg.muted"
          >
            <LuSearch aria-hidden />
            <Input
              aria-label="Find a token"
              placeholder="Find a token"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              variant="flushed"
              border="0"
              minH="44px"
            />
          </HStack>
        </HStack>
        <TokenTable rows={rows} denoms={denoms} empty={query ? "No token has that name." : "No tokens of this kind yet."} />
        <Text fontSize="sm" color="fg.muted">
          Raw names (factory/…, ibc/…) are in each token&apos;s More details.
        </Text>
      </Stack>
    </Stack>
  );
}

export function TokensSkeleton() {
  return (
    <Stack gap="6" aria-busy data-testid="tokens-skeleton">
      <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3">
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
    <Stack gap="6">
      <Heading as="h1" size={{ base: "2xl", md: "3xl" }}>
        Tokens
      </Heading>
      {data ? (
        <TokensView tokens={data} />
      ) : error ? (
        <Card title="The tokens could not be loaded">
          <HStack justify="space-between" wrap="wrap" gap="3">
            <Text color="fg.muted" fontSize="sm">
              The explorer did not answer. Try again in a moment.
            </Text>
            <Button colorPalette="blue" minH="44px" onClick={() => void refetch()}>
              Try again
            </Button>
          </HStack>
        </Card>
      ) : (
        <TokensSkeleton />
      )}
    </Stack>
  );
}
