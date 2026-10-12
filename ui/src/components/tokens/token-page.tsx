"use client";

import { Badge, Box, Button, Grid, Heading, HStack, Image, Link, SimpleGrid, Skeleton, SkeletonCircle, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import type { ReactNode } from "react";
import { LuExternalLink } from "react-icons/lu";

import { isNotFound } from "@/api/client";
import { useDenoms, useToken, useTokenEventsAfter, useTokenHolders, useTokenTransfers } from "@/api/hooks";
import type { LpPair, Token, TokenEvent } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { JsonBlock } from "@/components/ui/json-block";
import { MoreDetails } from "@/components/ui/more-details";
import { CardSkeleton, ListSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { Tile } from "@/components/ui/tile";
import { TimeAgo } from "@/components/ui/time-ago";
import { formatAmount, formatCount, formatDate, formatPct } from "@/lib/format";
import { externalLinks, websiteLink } from "@/lib/links";
import { dexAssetUrl } from "@/render/account";
import {
  activityItems,
  adminNote,
  baseUnitText,
  changeText,
  eventLine,
  headerChips,
  holdersSub,
  hopsExplainer,
  mergeEvents,
  mintBurnText,
  onDex,
  priceText,
  supplyState,
  supplyText,
  supplyTileSub,
  tokenHref,
  tokenSymbol,
  topHoldersText,
  transferLine,
  type ActivityItem,
  type Party,
} from "@/render/tokens";

import { TokenLogo } from "./tokens-page";

function Breadcrumb({ name }: { name: string }) {
  return (
    <HStack as="nav" aria-label="Breadcrumb" gap="2" fontSize="sm" color="fg.muted" minW="0">
      <Link asChild color="fg.muted">
        <NextLink href="/tokens">Tokens</NextLink>
      </Link>
      <Text as="span" aria-hidden>
        /
      </Text>
      <Text as="span" aria-current="page" truncate>
        {name}
      </Text>
    </HStack>
  );
}

function Chip({ party }: { party: Party }) {
  return <AccountChip address={party.address} label={party.label} />;
}

/** An LP token's two pool assets, each linking to its page. */
function PoolPair({ pair }: { pair: LpPair }) {
  const denoms = useDenoms();
  const side = (denom: string) => {
    const t = denoms.data?.get(denom);
    return (
      <Link asChild>
        <NextLink href={tokenHref(denom)}>{t ? tokenSymbol(t) : denom}</NextLink>
      </Link>
    );
  };
  return (
    <Text fontSize="sm" color="fg.muted" data-testid="pool-pair">
      A share of the DEX pool of {side(pair.base)} and {side(pair.quote)}.
    </Text>
  );
}

function Header({ t }: { t: Token }) {
  const denoms = useDenoms();
  const site = websiteLink(t.website);
  const symbol = tokenSymbol(t);
  return (
    <Stack gap="3">
      <Breadcrumb name={symbol} />
      <HStack gap="4" align="start" wrap="wrap" justify="space-between">
        <HStack gap="3" minW="0" align="start">
          <TokenLogo t={t} denoms={denoms.data} size="12" />
          <Stack gap="1.5" minW="0">
            <HStack gap="2" wrap="wrap" align="baseline">
              <Heading as="h1" size={{ base: "2xl", md: "3xl" }} wordBreak="break-word">
                {symbol}
              </Heading>
              {t.name && (
                <Text color="fg.muted" fontSize="lg">
                  {t.name}
                </Text>
              )}
            </HStack>
            <HStack gap="2" wrap="wrap" data-testid="token-chips">
              {headerChips(t).map((c) => (
                <Badge key={c.text} colorPalette={c.tone} variant="subtle" size="md" rounded="full" title={c.title}>
                  {c.logoUrl && <Image src={c.logoUrl} alt="" boxSize="4" rounded="full" />}
                  {c.text}
                </Badge>
              ))}
              {site && (
                <Link href={site.href} target="_blank" rel="noopener noreferrer nofollow" fontSize="sm">
                  {site.text} <LuExternalLink aria-hidden />
                </Link>
              )}
            </HStack>
            {hopsExplainer(t) && (
              <Text fontSize="sm" color="fg.muted" maxW="2xl" data-testid="hops-explainer">
                {hopsExplainer(t)}
              </Text>
            )}
            {t.lp_pair && <PoolPair pair={t.lp_pair} />}
            {t.kind === "factory" && t.creator && (
              <HStack gap="1.5" wrap="wrap" fontSize="sm" color="fg.muted" data-testid="token-created">
                <Text as="span">{t.created_time ? `Created ${formatDate(t.created_time)} by` : "Created by"}</Text>
                <AccountChip address={t.creator} label={t.creator_label} />
              </HStack>
            )}
          </Stack>
        </HStack>
        {onDex(t) && (
          <Button asChild colorPalette="blue" minH="44px">
            <a href={dexAssetUrl(t.denom)} target="_blank" rel="noopener noreferrer">
              View on the DEX <LuExternalLink aria-hidden />
            </a>
          </Button>
        )}
      </HStack>
    </Stack>
  );
}

function Tiles({ t }: { t: Token }) {
  const change = changeText(t.price_change_24h_pct);
  const symbol = tokenSymbol(t);
  return (
    <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3" data-testid="token-tiles">
      <Tile label="Total supply" value={supplyText(t)} sub={supplyTileSub(supplyState(t))} />
      <Tile
        label="Holders"
        value={t.holders_count != null ? formatCount(t.holders_count) : null}
        sub={holdersSub(t)}
      />
      <Tile
        label="Price"
        value={t.price_usd != null ? priceText(t.price_usd) : "no price"}
        sub={
          t.halted ? (
            <Text as="span" color="red.fg">
              halted on the DEX
            </Text>
          ) : t.price_usd != null ? (
            <>
              {change && (
                <Text as="span" color={`${change.tone}.fg`}>
                  {change.text}{" "}
                </Text>
              )}
              on the DEX
            </>
          ) : (
            "no market on the DEX"
          )
        }
      />
      <Tile
        label="Moved in 24h"
        value={formatAmount(t.transfers_24h.amount, t.exponent, symbol, { compact: true })}
        sub={`${formatCount(t.transfers_24h.count)} ${t.transfers_24h.count === 1 ? "transfer" : "transfers"}`}
      />
    </SimpleGrid>
  );
}

const SUPPLY_TONE = { admin: "orange", fixed: "green", elsewhere: "green", inflation: "blue", pool: "gray" } as const;

/** Says out loud whether the supply can change, and who decides. */
function SupplyBox({ t, events, complete }: { t: Token; events: TokenEvent[]; complete: boolean }) {
  const state = supplyState(t);
  if (!state) return null;
  const symbol = tokenSymbol(t);
  const chain = t.origin_chain?.name || t.origin_chain?.chain_id || "its own chain";
  let body: ReactNode;
  switch (state) {
    case "admin":
      body = (
        <>
          <Text fontWeight="semibold">The supply of {symbol} can change</Text>
          <Text as="div">
            The token&apos;s admin,{" "}
            <AccountChip address={t.admin!} label={t.admin_label} verticalAlign="middle" />, can create more {symbol} or
            destroy some at any time. {complete && mintBurnText(events, t)}
          </Text>
        </>
      );
      break;
    case "fixed":
      body = (
        <>
          <Text fontWeight="semibold">The supply of {symbol} is fixed</Text>
          <Text>The admin renounced: nobody can create more {symbol} or destroy any. {complete && mintBurnText(events, t)}</Text>
        </>
      );
      break;
    case "elsewhere":
      body = (
        <>
          <Text fontWeight="semibold">The supply of {symbol} is fixed on BZE</Text>
          <Text>
            {symbol} is minted on {chain}. Nobody on BZE can create more: it arrives over a cross-chain transfer and leaves the
            same way.
          </Text>
        </>
      );
      break;
    case "inflation":
      body = (
        <>
          <Text fontWeight="semibold">The supply of {symbol} grows with inflation</Text>
          <Text>
            New {symbol} is minted every block and paid to the people who stake.{" "}
            <Link asChild>
              <NextLink href="/parameters#inflation">See the inflation parameters</NextLink>
            </Link>
            .
          </Text>
        </>
      );
      break;
    case "pool":
      body = (
        <>
          <Text fontWeight="semibold">Pool shares follow the pool</Text>
          <Text>
            {symbol} is created when someone adds liquidity to its DEX pool and destroyed when it is taken out.
          </Text>
        </>
      );
      break;
  }
  const tone = SUPPLY_TONE[state];
  return (
    <Stack
      gap="1"
      p="4"
      rounded="l3"
      borderWidth="1px"
      borderColor={`${tone}.muted`}
      bg={`${tone}.subtle`}
      fontSize="sm"
      data-testid="supply-box"
      data-state={state}
    >
      {body}
    </Stack>
  );
}

/** A label/value row of a details list; stacks on phones. */
function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Grid templateColumns={{ base: "minmax(0, 1fr)", md: "180px minmax(0, 1fr)" }} gap={{ base: "0.5", md: "4" }} py="2">
      <Text fontSize="sm" color="fg.muted">
        {label}
      </Text>
      <Box fontSize="sm" minW="0" wordBreak="break-word">
        {children}
      </Box>
    </Grid>
  );
}

function About({ t }: { t: Token }) {
  return (
    <Card title="About">
      <Stack gap="0" data-testid="token-about">
        {t.name && <Row label="Name">{t.name}</Row>}
        {t.created_time && (
          <Row label="Created">
            {formatDate(t.created_time)}
            {t.created_tx_hash && (
              <>
                {" · "}
                <Link asChild>
                  <NextLink href={`/txs/${t.created_tx_hash.toUpperCase()}`}>see transaction</NextLink>
                </Link>
              </>
            )}
          </Row>
        )}
        {t.creator && (
          <Row label="Created by">
            <AccountChip address={t.creator} label={t.creator_label} />
          </Row>
        )}
        {t.kind === "factory" && (
          <Row label="Admin">
            <HStack gap="1.5" wrap="wrap">
              {t.admin && <AccountChip address={t.admin} label={t.admin_label} />}
              <Text as="span" color="fg.muted">
                {adminNote(t)}
              </Text>
            </HStack>
          </Row>
        )}
        <Row label="Decimals">{t.exponent}</Row>
        {t.kind === "ibc" && (
          <>
            <Row label="Origin chain">
              <HStack gap="1.5">
                {t.origin_chain?.logo_url && <Image src={t.origin_chain.logo_url} alt="" boxSize="5" rounded="full" />}
                <Text as="span">{t.origin_chain?.name || t.origin_chain?.chain_id || t.origin_chain_id || "not known yet"}</Text>
              </HStack>
            </Row>
            <Row label="Base denom there">
              <Text fontFamily="mono">{t.ibc_base_denom ?? "—"}</Text>
            </Row>
            <Row label="Path">
              <Text fontFamily="mono">{t.ibc_path ?? "—"}</Text>
            </Row>
          </>
        )}
        {t.description && (
          <Row label="Description">
            <Text whiteSpace="pre-line">{t.description}</Text>
          </Row>
        )}
      </Stack>
    </Card>
  );
}

const listItem = (i: number) => ({ borderTopWidth: i === 0 ? "0" : "1px", borderColor: "border.panel" }) as const;

function TopHolders({ t }: { t: Token }) {
  const holders = useTokenHolders(t.denom);
  const items = holders.data?.pages.flatMap((p) => p.items) ?? [];
  const symbol = tokenSymbol(t);
  return (
    <Card title="Top holders" meta={topHoldersText(items) ?? undefined}>
      {!holders.data ? (
        holders.error ? (
          <Text fontSize="sm" color="fg.muted">
            The holders could not be loaded.
          </Text>
        ) : (
          <ListSkeleton rows={5} />
        )
      ) : items.length === 0 ? (
        <Text fontSize="sm" color="fg.muted">
          Nobody holds {symbol} yet, or the holders were not counted yet.
        </Text>
      ) : (
        <Stack gap="3">
          <Stack gap="0" role="list" aria-label="Top holders">
            {items.map((h, i) => (
              <Grid
                key={h.address}
                role="listitem"
                templateColumns="32px minmax(0, 1fr) auto"
                gap="3"
                alignItems="center"
                py="2"
                minH="44px"
                fontSize="sm"
                {...listItem(i)}
                data-testid="holder-row"
              >
                <Text color="fg.muted">{h.rank}</Text>
                <Box minW="0">
                  <AccountChip address={h.address} label={h.label} />
                </Box>
                <Stack gap="0" align="end">
                  <Text fontVariantNumeric="tabular-nums">{formatAmount(h.balance, t.exponent, symbol, { compact: true })}</Text>
                  {h.share_pct != null && (
                    <Text fontSize="xs" color="fg.muted">
                      {formatPct(h.share_pct)}
                    </Text>
                  )}
                </Stack>
              </Grid>
            ))}
          </Stack>
          {holders.isFetchingNextPage && <ListSkeleton rows={5} />}
          {holders.hasNextPage && !holders.isFetchingNextPage && (
            <Button variant="outline" size="sm" minH="44px" alignSelf="start" onClick={() => void holders.fetchNextPage()}>
              {items.length <= 10 && t.holders_count != null
                ? `Show all ${formatCount(t.holders_count)} holders`
                : "Show more holders"}
            </Button>
          )}
        </Stack>
      )}
    </Card>
  );
}

function ActivityLine({ item, t }: { item: ActivityItem; t: Token }) {
  if (item.type === "event") {
    const line = eventLine(item.event, t);
    return (
      <HStack gap="1.5" wrap="wrap">
        <Text as="span">{line.text}</Text>
        {line.party && <Chip party={line.party} />}
      </HStack>
    );
  }
  const line = transferLine(item.transfer, t);
  switch (line.kind) {
    case "sent":
      return (
        <HStack gap="1.5" wrap="wrap">
          <Text as="span">Sent</Text>
          {line.from && <Chip party={line.from} />}
          <Text as="span" aria-hidden>
            →
          </Text>
          <Text as="span" fontWeight="medium">
            {line.amount}
          </Text>
          <Text as="span" aria-hidden>
            →
          </Text>
          {line.to && <Chip party={line.to} />}
        </HStack>
      );
    case "traded":
      return (
        <HStack gap="1.5" wrap="wrap">
          <Text as="span">Traded {line.amount} on the DEX</Text>
          {line.party && (
            <>
              <Text as="span">by</Text>
              <Chip party={line.party} />
            </>
          )}
        </HStack>
      );
    case "minted":
    case "burned":
      return (
        <HStack gap="1.5" wrap="wrap">
          <Text as="span">
            {line.kind === "minted" ? "Minted" : "Burned"} {line.amount} {line.party ? (line.kind === "minted" ? "to" : "from") : ""}
          </Text>
          {line.party && <Chip party={line.party} />}
        </HStack>
      );
  }
}

function RecentActivity({ t, events }: { t: Token; events: TokenEvent[] }) {
  const transfers = useTokenTransfers(t.denom);
  const loaded = transfers.data?.pages.flatMap((p) => p.items) ?? [];
  const items = activityItems(events, loaded, transfers.hasNextPage);
  return (
    <Card title="Recent activity">
      {!transfers.data && !transfers.error ? (
        <ListSkeleton rows={5} />
      ) : (
        <Stack gap="3">
          {items.length === 0 ? (
            <Text fontSize="sm" color="fg.muted">
              No activity indexed yet.
            </Text>
          ) : (
            <Stack gap="0" role="list" aria-label="Recent activity">
              {items.map((item, i) => (
                <Stack key={item.key} role="listitem" gap="0.5" py="2" fontSize="sm" {...listItem(i)} data-testid="activity-item">
                  <ActivityLine item={item} t={t} />
                  <HStack gap="1" color="fg.muted" fontSize="xs">
                    <TimeAgo time={item.time} />
                    {item.txHash && (
                      <>
                        <Text as="span">·</Text>
                        <Link asChild color="fg.muted">
                          <NextLink href={`/txs/${item.txHash.toUpperCase()}`}>see transaction</NextLink>
                        </Link>
                      </>
                    )}
                  </HStack>
                </Stack>
              ))}
            </Stack>
          )}
          {transfers.error && (
            <Text fontSize="sm" color="fg.muted">
              The transfers could not be loaded.
            </Text>
          )}
          {transfers.isFetchingNextPage && <ListSkeleton rows={5} />}
          {transfers.hasNextPage && !transfers.isFetchingNextPage && (
            <Button variant="outline" size="sm" minH="44px" alignSelf="start" onClick={() => void transfers.fetchNextPage()}>
              Show older activity
            </Button>
          )}
        </Stack>
      )}
    </Card>
  );
}

function Details({ t }: { t: Token }) {
  return (
    <Stack gap="4">
      <Card>
        <Stack gap="0">
          <Row label="Full name on chain">
            <HStack gap="1" align="start">
              <Text fontFamily="mono" wordBreak="break-all" data-testid="token-denom">
                {t.denom}
              </Text>
              <CopyButton value={t.denom} label="Copy full name" />
            </HStack>
          </Row>
          <Row label="Base unit">{baseUnitText(t)}</Row>
          <Row label="Branding">
            Its font and colours are set on the chain for the BZE apps; the explorer lists only when they change.
          </Row>
        </Stack>
      </Card>
      <Stack gap="2">
        <Heading as="h3" size="sm">
          Raw metadata
        </Heading>
        <JsonBlock value={t.metadata} />
      </Stack>
      <Link href={externalLinks.pingPub} target="_blank" rel="noopener noreferrer" fontSize="sm" alignSelf="start">
        Open on Ping.pub <LuExternalLink aria-hidden />
      </Link>
    </Stack>
  );
}

/** A found token. */
export function TokenView({ t }: { t: Token }) {
  const older = useTokenEventsAfter(t.denom, t.events_next_cursor);
  const events = mergeEvents(t.events, older.data);
  const complete = t.events_next_cursor == null || older.data != null;
  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Header t={t} />
      <Tiles t={t} />
      <SupplyBox t={t} events={events} complete={complete} />
      <About t={t} />
      <Grid templateColumns={{ base: "minmax(0, 1fr)", lg: "repeat(2, minmax(0, 1fr))" }} gap="6" alignItems="start">
        <TopHolders t={t} />
        <RecentActivity t={t} events={events} />
      </Grid>
      <MoreDetails hint="full name on chain, base unit, raw metadata">
        <Details t={t} />
      </MoreDetails>
    </Stack>
  );
}

export function TokenSkeleton() {
  return (
    <Stack gap="6" aria-busy data-testid="token-skeleton">
      <Stack gap="3">
        <Skeleton h="4" w="32" />
        <HStack gap="3">
          <SkeletonCircle size="12" />
          <Stack gap="2" flex="1">
            <Skeleton h="8" w="48" maxW="full" />
            <Skeleton h="5" w="64" maxW="full" />
          </Stack>
        </HStack>
      </Stack>
      <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3">
        {Array.from({ length: 4 }).map((_, i) => (
          <TileSkeleton key={i} />
        ))}
      </SimpleGrid>
      <CardSkeleton lines={5} />
      <Grid templateColumns={{ base: "minmax(0, 1fr)", lg: "repeat(2, minmax(0, 1fr))" }} gap="6">
        <ListSkeleton rows={5} />
        <ListSkeleton rows={5} />
      </Grid>
    </Stack>
  );
}

/** `/tokens/[denom]`: the skeleton, then the token (polled), not found on a 404, or the error state. */
export function TokenPage({ denom }: { denom: string }) {
  const { data, error, refetch } = useToken(denom);
  if (data) return <TokenView t={data} />;
  if (isNotFound(error)) {
    return (
      <Card title="Token not found">
        <Text fontSize="sm" color="fg.muted">
          The explorer does not know this token. Check the link, or find it in the{" "}
          <Link asChild>
            <NextLink href="/tokens">tokens list</NextLink>
          </Link>
          .
        </Text>
      </Card>
    );
  }
  if (error) {
    return (
      <Card title="This token could not be loaded">
        <HStack justify="space-between" wrap="wrap" gap="3">
          <Text color="fg.muted" fontSize="sm">
            The explorer did not answer. Try again in a moment.
          </Text>
          <Button colorPalette="blue" minH="44px" onClick={() => void refetch()}>
            Try again
          </Button>
        </HStack>
      </Card>
    );
  }
  return <TokenSkeleton />;
}
