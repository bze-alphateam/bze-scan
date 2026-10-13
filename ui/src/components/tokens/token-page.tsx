"use client";

import { Box, Button, Flex, Grid, Heading, HStack, Image, Link, SimpleGrid, Skeleton, SkeletonCircle, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import type { ReactNode } from "react";
import { LuCircleHelp, LuExternalLink, LuTriangleAlert } from "react-icons/lu";

import { isNotFound } from "@/api/client";
import { useDenoms, useToken, useTokenEventsAfter, useTokenHolders, useTokenTransfers } from "@/api/hooks";
import type { LpPair, OriginChain, Token, TokenEvent } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Callout, type CalloutTone } from "@/components/ui/callout";
import { Chip } from "@/components/ui/chip";
import { CopyButton } from "@/components/ui/copy-button";
import { DetailList, DetailRow } from "@/components/ui/detail-list";
import { ExplainedBadges } from "@/components/ui/explained-badge";
import { JsonBlock } from "@/components/ui/json-block";
import { ListPanel, ListRow } from "@/components/ui/list";
import { MoreDetails } from "@/components/ui/more-details";
import { Breadcrumb as Breadcrumbs, DetailMeta, DetailTitle, PanelHead } from "@/components/ui/page-head";
import { CardSkeleton, ListSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { Tile } from "@/components/ui/tile";
import { TimeAgo } from "@/components/ui/time-ago";
import { formatAmount, formatCount, formatDate, formatPct } from "@/lib/format";
import { externalLinks, websiteLink } from "@/lib/links";
import { dexAssetUrl, multiHop } from "@/render/account";
import { connectionChip, headerChips, isDeadChain, strandedExplainer } from "@/render/badges";
import {
  activityItems,
  adminNote,
  baseUnitText,
  changeText,
  eventLine,
  holdersSub,
  ibcRoute,
  mergeEvents,
  mintBurnText,
  multiHopCallout,
  onDex,
  priceText,
  routeNote,
  shortDenom,
  supplyState,
  supplyText,
  supplyTileSub,
  tokenHref,
  tokenSymbol,
  topHoldersText,
  transferLine,
  type ActivityItem,
  type IbcRoute,
  type Party,
  type RouteStop,
  type SupplyState,
} from "@/render/tokens";

import { TokenLogo } from "./tokens-page";

// Custom breakpoints are not in Chakra's generated types, so responsive objects using them are hoisted.
const ROUTE_DIRECTION = { base: "column", c2: "row" } as const;
const LINK_MAX_W = { base: "none", c2: "220px" };
const LINK_MIN_H = { base: "56px", c2: "0" };
const TWO_COLUMNS = { base: "minmax(0, 1fr)", lg: "repeat(2, minmax(0, 1fr))" };
const TILE_COLUMNS = { base: 1, sm: 2, lg: 4 };

const Muted = ({ children }: { children: ReactNode }) => (
  <Text as="span" color="fg.muted">
    {children}
  </Text>
);

const Mono = ({ children }: { children: ReactNode }) => (
  <Text as="span" fontFamily="mono">
    {children}
  </Text>
);

/** "Tokens / HONEY". */
function Breadcrumb({ current, mono }: { current: string; mono?: boolean }) {
  return <Breadcrumbs path={[{ label: "Tokens", href: "/tokens" }]} current={current} mono={mono} />;
}

const chainName = (c: OriginChain | null, fallback: string) => c?.name || c?.chain_id || fallback;

/** An origin chain's outlined chip, "(shut down)" when the registry says so. */
function ChainChip({ chain, fallback, look = "ch" }: { chain: OriginChain | null; fallback: string; look?: "ch" | "via" }) {
  return (
    <Chip variant={look}>
      {chain?.logo_url && <Image src={chain.logo_url} alt="" boxSize="3" rounded="full" />}
      {chainName(chain, fallback)}
    </Chip>
  );
}

/** An LP token's two pool assets, each linking to its page. */
function PoolPair({ pair }: { pair: LpPair }) {
  const denoms = useDenoms();
  const side = (denom: string) => {
    const t = denoms.data?.get(denom);
    return (
      <Link asChild color="accent.fg">
        <NextLink href={tokenHref(denom)}>{t ? tokenSymbol(t) : denom}</NextLink>
      </Link>
    );
  };
  return (
    <Text data-testid="pool-pair">
      A share of the DEX pool of {side(pair.base)} and {side(pair.quote)}.
    </Text>
  );
}

function Header({ t }: { t: Token }) {
  const denoms = useDenoms();
  const site = websiteLink(t.website);
  const symbol = tokenSymbol(t);
  return (
    <Box>
      <Breadcrumb current={symbol} />
      <Flex gap="4" align="flex-start" wrap="wrap" mt="3">
        <TokenLogo t={t} denoms={denoms.data} size="56px" />
        <Box flex="1" minW="240px">
          <DetailTitle title={symbol}>
            {t.name && (
              <Text color="fg.muted" fontSize="18px">
                {t.name}
              </Text>
            )}
          </DetailTitle>
          <DetailMeta data-testid="token-chips">
            <ExplainedBadges badges={headerChips(t)} />
            {site && (
              <Link href={site.href} target="_blank" rel="noopener noreferrer nofollow" color="accent.fg">
                {site.text} ↗
              </Link>
            )}
          </DetailMeta>
          {t.kind === "factory" && t.creator && (
            <DetailMeta mt="6px" gap="2" data-testid="token-created">
              <Text as="span">{t.created_time ? `Created ${formatDate(t.created_time)} by` : "Created by"}</Text>
              <AccountChip address={t.creator} label={t.creator_label} />
              <CopyButton value={t.creator} label="Copy creator address" />
            </DetailMeta>
          )}
          {t.lp_pair && (
            <DetailMeta mt="6px">
              <PoolPair pair={t.lp_pair} />
            </DetailMeta>
          )}
        </Box>
        {onDex(t) && (
          <Button asChild colorPalette="blue" h="40px" px="4" fontWeight="600">
            <a href={dexAssetUrl(t.denom)} target="_blank" rel="noopener noreferrer">
              View on the DEX <LuExternalLink aria-hidden />
            </a>
          </Button>
        )}
      </Flex>
    </Box>
  );
}

/** The warnings under the header: a multi-hop token is not the direct one; a stranded one cannot go back. */
function Warnings({ t }: { t: Token }) {
  const hop = multiHopCallout(t);
  const stranded = strandedExplainer(t);
  if (!hop && !stranded) return null;
  return (
    <Stack gap="3" mt="5">
      {hop && (
        <Callout tone="warn" title={hop.title} data-testid="hops-callout">
          {hop.body}
        </Callout>
      )}
      {stranded && (
        <Callout tone="warn" title={`${tokenSymbol(t)} is stranded on BZE`} data-testid="stranded-explainer">
          {stranded}
        </Callout>
      )}
    </Stack>
  );
}

function Tiles({ t }: { t: Token }) {
  const change = changeText(t.price_change_24h_pct);
  const symbol = tokenSymbol(t);
  const tone = change ? { green: "pos.fg", red: "neg.fg", gray: "fg.muted" }[change.tone] : undefined;
  return (
    <SimpleGrid columns={TILE_COLUMNS} gap="3" mt="6" data-testid="token-tiles">
      <Tile label="Total supply" value={supplyText(t)} sub={supplyTileSub(supplyState(t))} />
      <Tile label="Holders" value={t.holders_count != null ? formatCount(t.holders_count) : null} sub={holdersSub(t)} />
      <Tile
        label="Price"
        value={
          t.price_usd != null ? (
            priceText(t.price_usd)
          ) : (
            <Text as="span" color="fg.muted">
              no price
            </Text>
          )
        }
        sub={
          t.halted ? (
            <Text as="span" color="neg.fg">
              halted on the DEX
            </Text>
          ) : t.price_usd != null ? (
            <>
              {change && (
                <Text as="span" fontFamily="mono" color={tone}>
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
        sub={
          <>
            <Mono>{formatCount(t.transfers_24h.count)}</Mono> {t.transfers_24h.count === 1 ? "transfer" : "transfers"}
          </>
        }
      />
    </SimpleGrid>
  );
}

const SUPPLY_TONE: Record<SupplyState, CalloutTone> = { admin: "warn", fixed: "ok", elsewhere: "ok", inflation: "acc", pool: "acc" };

/** Says out loud whether the supply can change, and who decides. */
function SupplyCallout({ t, events, complete }: { t: Token; events: TokenEvent[]; complete: boolean }) {
  const state = supplyState(t);
  if (!state) return null;
  const symbol = tokenSymbol(t);
  const chain = chainName(t.origin_chain, "its own chain");
  const via = multiHop(t)?.via;
  const body: Record<SupplyState, { title: string; text: ReactNode }> = {
    admin: {
      title: `The supply of ${symbol} can change`,
      text: (
        <>
          The token&apos;s admin, <AccountChip address={t.admin ?? ""} label={t.admin_label} verticalAlign="middle" />, can create more {symbol}{" "}
          or destroy some at any time. {complete && mintBurnText(events, t)}
        </>
      ),
    },
    fixed: {
      title: `The supply of ${symbol} is fixed`,
      text: (
        <>
          The admin renounced: nobody can create more {symbol} or destroy any. {complete && mintBurnText(events, t)}
        </>
      ),
    },
    elsewhere: {
      title: `The supply of ${symbol} is fixed on BZE`,
      text: `${symbol} is minted on ${chain}${via ? ` and reached BZE through ${via}` : ""}. Nobody on BZE can create more: it arrives over a cross-chain transfer and leaves the same way.`,
    },
    inflation: {
      title: `The supply of ${symbol} grows with inflation`,
      text: (
        <>
          New {symbol} is minted every block and paid to the people who stake.{" "}
          <Link asChild color="accent.fg">
            <NextLink href="/parameters#inflation">See the inflation parameters</NextLink>
          </Link>
          .
        </>
      ),
    },
    pool: {
      title: "Pool shares follow the pool",
      text: `${symbol} is created when someone adds liquidity to its DEX pool and destroyed when it is taken out.`,
    },
  };
  return (
    <Callout tone={SUPPLY_TONE[state]} title={body[state].title} mt="4" data-testid="supply-box" data-state={state}>
      {body[state].text}
    </Callout>
  );
}

const STOP_LOOK: Record<RouteStop["role"], { border: string; borderStyle?: string; bg: string; roleColor: string; role: string }> = {
  origin: { border: "accent.fg", bg: "bg", roleColor: "accent.fg", role: "minted here" },
  through: { border: "border.panel", bg: "bg", roleColor: "fg.muted", role: "passed through" },
  counted: { border: "fg.muted", borderStyle: "dashed", bg: "bg", roleColor: "fg.muted", role: "not named by the explorer" },
  here: { border: "pos.fg", bg: "pos.subtle", roleColor: "pos.fg", role: "this token" },
};

function Stop({ stop }: { stop: RouteStop }) {
  const look = STOP_LOOK[stop.role];
  return (
    <Flex
      direction="column"
      gap="3px"
      flex="none"
      minW="130px"
      px="14px"
      py="10px"
      borderWidth="1px"
      borderStyle={look.borderStyle}
      borderColor={look.border}
      rounded="l2"
      bg={look.bg}
      data-testid="route-stop"
      data-role={stop.role}
    >
      <Text as="span" fontWeight="600">
        {stop.name}
      </Text>
      <Text as="span" fontSize="11px" textTransform="uppercase" letterSpacing="0.06em" color={look.roleColor}>
        {look.role}
      </Text>
      {stop.detail && (
        <Text as="span" fontFamily="mono" fontSize="12px" color="fg.muted" wordBreak="break-all">
          {stop.detail}
        </Text>
      )}
    </Flex>
  );
}

/** "How it got here": the chains from the token's home to BZE, each link with its channel. */
function Route({ t, route }: { t: Token; route: IbcRoute }) {
  const state = t.ibc_channel ? connectionChip(t.ibc_channel).text.toLowerCase() : null;
  return (
    <ListPanel as="section" aria-labelledby="token-route" mt="7" data-testid="ibc-route">
      <PanelHead id="token-route" title="How it got here" note={routeNote(route.hops)} />
      <Flex direction={ROUTE_DIRECTION} align="stretch" p="4" overflowX="auto">
        {route.stops.map((stop, i) => {
          const link = route.links[i];
          return (
            <Flex key={i} direction={ROUTE_DIRECTION} align="stretch" flex={link ? "1" : "none"}>
              <Stop stop={stop} />
              {link && (
                <Flex
                  direction="column"
                  justify="center"
                  align="center"
                  gap="2px"
                  flex="1"
                  minW="110px"
                  maxW={LINK_MAX_W}
                  minH={LINK_MIN_H}
                  px="10px"
                  fontSize="12px"
                  color="fg.muted"
                  data-testid="route-link"
                >
                  <Text as="span" fontFamily="mono">
                    {link.channels.join(", ")}
                  </Text>
                  <Box hideBelow="c2" w="full" borderTopWidth="1px" borderStyle="dashed" borderColor="fg.muted" my="6px" position="relative" aria-hidden>
                    <Text as="span" position="absolute" right="-2px" top="-11px" fontSize="14px">
                      →
                    </Text>
                  </Box>
                  <Text as="span" hideFrom="c2" fontSize="14px" aria-hidden>
                    ↓
                  </Text>
                  <Text as="span" fontSize="11px">
                    on {link.on}
                    {link.on === "BZE" && state ? ` · ${state}` : ""}
                  </Text>
                </Flex>
              )}
            </Flex>
          );
        })}
      </Flex>
    </ListPanel>
  );
}

/** A cross-chain token's About rows: where it was minted, the way it came, its channels. */
function ibcRows(t: Token, route: IbcRoute | null): [string, ReactNode][] {
  const hop = multiHop(t);
  const home = (
    <>
      <ChainChip chain={t.origin_chain} fallback={t.origin_chain_id || "not known yet"} />
      {isDeadChain(t.origin_chain) && (
        <Chip variant="bad" data-testid="chain-dead">
          shut down
        </Chip>
      )}
    </>
  );
  const rows: [string, ReactNode][] = [];
  if (hop) {
    rows.push([
      "Minted on",
      <>
        {home}
        {t.ibc_base_denom && (
          <>
            <Muted>as</Muted> <Mono>{t.ibc_base_denom}</Mono>
          </>
        )}
      </>,
    ]);
    rows.push([
      "Came through",
      <>
        <ChainChip chain={t.via_chain} fallback="another chain" look="via" />
        <Muted>· {formatCount(hop.hops)} hops, not direct</Muted>
      </>,
    ]);
  } else {
    rows.push(["Origin chain", home]);
    rows.push(["Base denom there", <Mono key="base">{t.ibc_base_denom ?? "—"}</Mono>]);
  }
  if (!route) rows.push(["Path", <Mono key="path">{t.ibc_path ?? "—"}</Mono>]);
  else if (!hop) {
    const p = route.paths[0];
    rows.push([
      "Path",
      <Box key="path" data-testid="ibc-path">
        <Mono>{p.channel}</Mono> <Muted>on BZE, to {p.toward}</Muted>
      </Box>,
    ]);
  } else {
    for (const p of route.paths) {
      rows.push([
        `Path on ${p.on}`,
        <Box key={p.channel} data-testid="ibc-path">
          <Mono>{p.channel}</Mono> <Muted>· {p.on} to {p.toward}</Muted>
        </Box>,
      ]);
    }
  }
  if (t.ibc_channel) {
    const c = connectionChip(t.ibc_channel);
    rows.push([
      "Connection",
      <Flex key="connection" align="center" gap="2" wrap="wrap" data-testid="ibc-connection">
        <Mono>{t.ibc_channel.channel_id}</Mono>
        <Chip variant={c.look}>{c.text}</Chip>
        {c.note && <Muted>{c.note}</Muted>}
      </Flex>,
    ]);
  }
  return rows;
}

function About({ t, route }: { t: Token; route: IbcRoute | null }) {
  const rows: [string, ReactNode][] = [];
  if (t.name) rows.push(["Name", t.name]);
  if (t.created_time) {
    rows.push([
      "Created",
      <>
        {formatDate(t.created_time)}
        {t.created_tx_hash && (
          <>
            <Muted>·</Muted>
            <Link asChild color="accent.fg">
              <NextLink href={`/txs/${t.created_tx_hash.toUpperCase()}`}>see transaction</NextLink>
            </Link>
          </>
        )}
      </>,
    ]);
  }
  if (t.creator) {
    rows.push([
      "Created by",
      <>
        <AccountChip address={t.creator} label={t.creator_label} />
        <CopyButton value={t.creator} label="Copy creator address" />
      </>,
    ]);
  }
  if (t.kind === "factory") {
    rows.push([
      "Admin",
      <>
        {t.admin ? <AccountChip address={t.admin} label={t.admin_label} /> : <Chip variant="ok">Renounced</Chip>}
        <Muted>{t.admin ? adminNote(t) : "nobody can mint or burn"}</Muted>
      </>,
    ]);
  }
  rows.push(["Decimals", <Mono key="decimals">{t.exponent}</Mono>]);
  if (t.kind === "ibc") rows.push(...ibcRows(t, route));
  if (t.description) {
    rows.push([
      "Description",
      <Text key="description" as="span" whiteSpace="pre-line">
        {t.description}
      </Text>,
    ]);
  }
  return (
    <ListPanel as="section" aria-labelledby="token-about-head" mt="7" data-testid="token-about">
      <PanelHead id="token-about-head" title="About" />
      <DetailList>
        {rows.map(([label, value], i) => (
          <DetailRow key={`${label}-${i}`} label={label} last={i === rows.length - 1}>
            {value}
          </DetailRow>
        ))}
      </DetailList>
    </ListPanel>
  );
}

/** A section that failed on its own: the red line and Try again, inside its panel. */
function SectionError({ what, onRetry }: { what: string; onRetry: () => void }) {
  return (
    <Flex align="center" gap="2" wrap="wrap" px="4" py="14px" fontSize="13px" data-testid="section-error">
      <Text as="span" color="neg.fg">
        {what} could not be loaded.
      </Text>
      <Button variant="plain" size="xs" h="auto" p="0" color="accent.fg" fontSize="13px" fontWeight="500" onClick={onRetry}>
        Try again
      </Button>
    </Flex>
  );
}

/** The quiet button under a panel's rows: more holders, older activity. */
function PanelFoot({ children, onClick }: { children: ReactNode; onClick: () => void }) {
  return (
    <Box px="4" py="10px" borderTopWidth="1px" borderColor="border.panel">
      <Button variant="outline" size="xs" h="30px" px="3" fontSize="12px" onClick={onClick}>
        {children}
      </Button>
    </Box>
  );
}

function TopHolders({ t }: { t: Token }) {
  const holders = useTokenHolders(t.denom);
  const items = holders.data?.pages.flatMap((p) => p.items) ?? [];
  const symbol = tokenSymbol(t);
  return (
    <ListPanel as="section" aria-labelledby="token-holders">
      <PanelHead id="token-holders" title="Top holders" note={topHoldersText(items) ?? undefined} />
      {!holders.data ? (
        holders.error ? (
          <SectionError what="The holders" onRetry={() => void holders.refetch()} />
        ) : (
          <Box px="4" py="2">
            <ListSkeleton rows={5} />
          </Box>
        )
      ) : items.length === 0 ? (
        <Text px="4" py="14px" fontSize="13px" color="fg.muted">
          Nobody holds {symbol} yet, or the holders were not counted yet.
        </Text>
      ) : (
        <>
          <Box role="list" aria-label="Top holders">
            {items.map((h) => (
              <ListRow key={h.address} role="listitem" templateColumns="28px minmax(0, 1fr) auto" gap="3" data-testid="holder-row">
                <Text fontFamily="mono" color="fg.muted">
                  {h.rank}
                </Text>
                <Box minW="0">
                  <AccountChip address={h.address} label={h.label} />
                </Box>
                <Box textAlign="right">
                  <Text fontFamily="mono">{formatAmount(h.balance, t.exponent, symbol, { compact: true })}</Text>
                  {h.share_pct != null && (
                    <Text fontFamily="mono" fontSize="12px" color="fg.muted">
                      {formatPct(h.share_pct)}
                    </Text>
                  )}
                </Box>
              </ListRow>
            ))}
          </Box>
          {holders.isFetchingNextPage && (
            <Box px="4" py="2">
              <ListSkeleton rows={5} />
            </Box>
          )}
          {holders.hasNextPage && !holders.isFetchingNextPage && (
            <PanelFoot onClick={() => void holders.fetchNextPage()}>
              {items.length <= 10 && t.holders_count != null ? `Show all ${formatCount(t.holders_count)} holders` : "Show more holders"}
            </PanelFoot>
          )}
        </>
      )}
    </ListPanel>
  );
}

function ActivityLine({ item, t }: { item: ActivityItem; t: Token }) {
  if (item.type === "event") {
    const line = eventLine(item.event, t);
    return (
      <HStack gap="1.5" wrap="wrap">
        <Text as="span">{line.text}</Text>
        {line.party && <PartyChip party={line.party} />}
      </HStack>
    );
  }
  const line = transferLine(item.transfer, t);
  switch (line.kind) {
    case "sent":
      return (
        <HStack gap="1.5" wrap="wrap">
          <Muted>Sent</Muted>
          {line.from && <PartyChip party={line.from} />}
          <Text as="span" color="fg.muted" aria-hidden>
            →
          </Text>
          <Text as="span" fontFamily="mono" fontWeight="500">
            {line.amount}
          </Text>
          <Text as="span" color="fg.muted" aria-hidden>
            →
          </Text>
          {line.to && <PartyChip party={line.to} />}
        </HStack>
      );
    case "traded":
      return (
        <HStack gap="1.5" wrap="wrap">
          <Text as="span">
            Traded{" "}
            <Text as="span" fontFamily="mono" fontWeight="500">
              {line.amount}
            </Text>{" "}
            on the DEX
          </Text>
          {line.party && (
            <>
              <Muted>by</Muted>
              <PartyChip party={line.party} />
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
          {line.party && <PartyChip party={line.party} />}
        </HStack>
      );
  }
}

const PartyChip = ({ party }: { party: Party }) => <AccountChip address={party.address} label={party.label} />;

function RecentActivity({ t, events }: { t: Token; events: TokenEvent[] }) {
  const transfers = useTokenTransfers(t.denom);
  const loaded = transfers.data?.pages.flatMap((p) => p.items) ?? [];
  const items = activityItems(events, loaded, transfers.hasNextPage);
  const failed = !!transfers.error && !transfers.isFetching;
  return (
    <ListPanel as="section" aria-labelledby="token-activity">
      <PanelHead id="token-activity" title="Recent activity" />
      {!transfers.data && !transfers.error ? (
        <Box px="4" py="2">
          <ListSkeleton rows={5} />
        </Box>
      ) : (
        <>
          {items.length > 0 && (
            <Box role="list" aria-label="Recent activity">
              {items.map((item) => (
                <Box
                  key={item.key}
                  role="listitem"
                  px="4"
                  py="10px"
                  fontSize="13px"
                  borderBottomWidth="1px"
                  borderColor="border.panel"
                  _last={{ borderBottomWidth: "0" }}
                  data-testid="activity-item"
                >
                  <ActivityLine item={item} t={t} />
                  <HStack gap="1" color="fg.muted" fontSize="12px" mt="3px">
                    <TimeAgo time={item.time} />
                    {item.txHash && (
                      <>
                        <Text as="span">·</Text>
                        <Link asChild color="accent.fg">
                          <NextLink href={`/txs/${item.txHash.toUpperCase()}`}>see transaction</NextLink>
                        </Link>
                      </>
                    )}
                  </HStack>
                </Box>
              ))}
            </Box>
          )}
          {failed ? (
            <Box borderTopWidth={items.length > 0 ? "1px" : "0"} borderColor="border.panel">
              <SectionError what="The transfers" onRetry={() => void transfers.refetch()} />
            </Box>
          ) : (
            items.length === 0 && (
              <Text px="4" py="14px" fontSize="13px" color="fg.muted">
                No activity indexed yet.
              </Text>
            )
          )}
          {transfers.isFetchingNextPage && (
            <Box px="4" py="2">
              <ListSkeleton rows={5} />
            </Box>
          )}
          {transfers.hasNextPage && !transfers.isFetchingNextPage && (
            <PanelFoot onClick={() => void transfers.fetchNextPage()}>Show older activity</PanelFoot>
          )}
        </>
      )}
    </ListPanel>
  );
}

function PingPubOpen(props: { hideBelow?: "c2"; hideFrom?: "c2"; display?: string; px?: string; pb?: string }) {
  return (
    <Link href={externalLinks.pingPub} target="_blank" rel="noopener noreferrer" fontSize="12px" color="accent.fg" whiteSpace="nowrap" {...props}>
      Open on Ping.pub ↗
    </Link>
  );
}

function Details({ t }: { t: Token }) {
  return (
    <ListPanel>
      <DetailList>
        <DetailRow label="Full name on chain">
          <Text as="span" fontFamily="mono" fontSize="12px" wordBreak="break-all" data-testid="token-denom">
            {t.denom}
          </Text>
          <CopyButton value={t.denom} label="Copy full name" />
        </DetailRow>
        <DetailRow label="Base unit">
          <Mono>{baseUnitText(t)}</Mono>
        </DetailRow>
        <DetailRow label="Branding" last>
          <Muted>Its font and colours are set on the chain for the BZE apps; the explorer lists only when they change.</Muted>
        </DetailRow>
      </DetailList>
      <HStack gap="0.5" px="2" borderTopWidth="1px" borderColor="border.panel">
        <Heading as="h3" px="2.5" py="10px" fontSize="13px" fontWeight="500" borderBottomWidth="2px" borderColor="accent.fg" whiteSpace="nowrap">
          Raw metadata
        </Heading>
        <Box flex="1" />
        <PingPubOpen hideBelow="c2" px="2" />
      </HStack>
      <Box p="4" pt="3" borderTopWidth="1px" borderColor="border.panel">
        <JsonBlock value={t.metadata} />
      </Box>
      <PingPubOpen hideFrom="c2" display="inline-block" px="4" pb="4" />
    </ListPanel>
  );
}

/** A found token. */
export function TokenView({ t }: { t: Token }) {
  const older = useTokenEventsAfter(t.denom, t.events_next_cursor);
  const events = mergeEvents(t.events, older.data);
  const complete = t.events_next_cursor == null || older.data != null;
  const route = ibcRoute(t);
  return (
    <Box>
      <Header t={t} />
      <Warnings t={t} />
      <Tiles t={t} />
      <SupplyCallout t={t} events={events} complete={complete} />
      {route && <Route t={t} route={route} />}
      <About t={t} route={route} />
      <Grid templateColumns={TWO_COLUMNS} gap="4" alignItems="start" mt="8">
        <TopHolders t={t} />
        <RecentActivity t={t} events={events} />
      </Grid>
      <Box mt="8">
        <MoreDetails hint="full name on chain, base unit, raw metadata">
          <Details t={t} />
        </MoreDetails>
      </Box>
    </Box>
  );
}

export function TokenSkeleton() {
  return (
    <Stack gap="6" aria-busy data-testid="token-skeleton">
      <Stack gap="3" pt="6">
        <Skeleton h="4" w="32" />
        <HStack gap="4">
          <SkeletonCircle size="14" />
          <Stack gap="2" flex="1">
            <Skeleton h="8" w="48" maxW="full" />
            <Skeleton h="5" w="64" maxW="full" />
          </Stack>
        </HStack>
      </Stack>
      <SimpleGrid columns={TILE_COLUMNS} gap="3">
        {Array.from({ length: 4 }).map((_, i) => (
          <TileSkeleton key={i} />
        ))}
      </SimpleGrid>
      <CardSkeleton lines={5} />
      <Grid templateColumns={TWO_COLUMNS} gap="4">
        <ListSkeleton rows={5} />
        <ListSkeleton rows={5} />
      </Grid>
    </Stack>
  );
}

/** The breadcrumb and "Token" over a state box: not found, or the load error. */
function Missing({ denom, children }: { denom: string; children: ReactNode }) {
  return (
    <Box>
      <Breadcrumb current={shortDenom(denom)} mono />
      <DetailTitle title="Token" />
      <Box mt="6">{children}</Box>
    </Box>
  );
}

/** `/tokens/[denom]`: the skeleton, then the token (polled), not found on a 404, or the error state. */
export function TokenPage({ denom }: { denom: string }) {
  const { data, error, refetch } = useToken(denom);
  if (data) return <TokenView t={data} />;
  if (isNotFound(error)) {
    return (
      <Missing denom={denom}>
        <StateBox icon={<LuCircleHelp size={20} />} title="Token not found" data-testid="token-not_found">
          The explorer does not know this token. Check the link, or find it in the{" "}
          <Link asChild color="accent.fg">
            <NextLink href="/tokens">tokens list</NextLink>
          </Link>
          .
        </StateBox>
      </Missing>
    );
  }
  if (error) {
    return (
      <Missing denom={denom}>
        <StateBox
          tone="error"
          icon={<LuTriangleAlert size={20} />}
          title="This token could not be loaded"
          actions={
            <Button size="sm" colorPalette="blue" onClick={() => void refetch()}>
              Try again
            </Button>
          }
          data-testid="token-error"
        >
          The explorer did not answer. Try again in a moment.
        </StateBox>
      </Missing>
    );
  }
  return <TokenSkeleton />;
}
