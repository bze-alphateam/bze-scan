"use client";

import { Box, Heading, HStack, Link, SimpleGrid, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import type { ReactNode } from "react";
import { LuArrowRight, LuExternalLink } from "react-icons/lu";

import { useBlocks, useStats, useTransactions } from "@/api/hooks";
import type { BlockSummary, TxSummary } from "@/api/types";
import { SearchBox } from "@/components/layout/search-box";
import { useListRenderContext } from "@/components/txs/list-context";
import { AccountChip } from "@/components/ui/account-chip";
import { HashLink } from "@/components/ui/links";
import { ListSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { Tile } from "@/components/ui/tile";
import { TimeAgo } from "@/components/ui/time-ago";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount } from "@/lib/format";
import { appLinks } from "@/lib/links";
import { homeTiles } from "@/render/home";
import type { Counterparty, RenderContext } from "@/render/messages/types";
import { txCounterparty, txOneLine } from "@/render/txs";

const panel = {
  bg: "bg.panel",
  borderWidth: "1px",
  borderColor: "border.panel",
  rounded: "l3",
} as const;

const TONE = { up: "green.fg", down: "red.fg", flat: "fg.muted" } as const;

/** The six tiles: skeletons on the first load, the last values kept on an error, "—" when never loaded. */
function Tiles() {
  const stats = useStats();
  if (!stats.data && !stats.error) {
    return (
      <SimpleGrid columns={{ base: 2, md: 3, lg: 6 }} gap="3">
        {Array.from({ length: 6 }).map((_, i) => (
          <TileSkeleton key={i} />
        ))}
      </SimpleGrid>
    );
  }
  return (
    <SimpleGrid columns={{ base: 2, md: 3, lg: 6 }} gap="3" data-testid="home-tiles">
      {homeTiles(stats.data).map((t) => (
        <Tile
          key={t.label}
          label={t.label}
          value={t.value}
          sub={
            t.time ? (
              <TimeAgo time={t.time} />
            ) : t.sub ? (
              <Text as="span" color={t.tone ? TONE[t.tone] : undefined}>
                {t.sub}
              </Text>
            ) : null
          }
        />
      ))}
    </SimpleGrid>
  );
}

/** A home list: a title, "View all" and its rows. */
function Latest({ title, href, id, children }: { title: string; href: string; id: string; children: ReactNode }) {
  return (
    <Stack as="section" gap="3" minW="0" aria-labelledby={id} data-testid={id}>
      <HStack justify="space-between">
        <Heading as="h2" size="lg" id={id}>
          {title}
        </Heading>
        <Link asChild fontSize="sm" minH="44px">
          <NextLink href={href} aria-label={`View all ${title.replace(/^Latest /, "")}`}>
            View all <LuArrowRight />
          </NextLink>
        </Link>
      </HStack>
      {children}
    </Stack>
  );
}

function Unavailable({ what }: { what: string }) {
  return (
    <Box {...panel} p="4">
      <Text fontSize="sm" color="fg.muted">
        The latest {what} could not be loaded. They will appear as soon as the explorer answers.
      </Text>
    </Box>
  );
}

function Row({ children, testId }: { children: ReactNode; testId: string }) {
  return (
    <Stack role="listitem" gap="1" px="4" py="3" minH="64px" justify="center" borderTopWidth="1px" borderColor="border.panel" _first={{ borderTopWidth: "0" }} data-testid={testId}>
      {children}
    </Stack>
  );
}

function BlockRow({ b }: { b: BlockSummary }) {
  return (
    <Row testId="latest-block">
      <HStack justify="space-between" gap="3">
        <Link asChild fontWeight="medium" fontVariantNumeric="tabular-nums">
          <NextLink href={`/blocks/${b.height}`}>{formatCount(b.height)}</NextLink>
        </Link>
        <TimeAgo time={b.time} fontSize="sm" color="fg.muted" />
      </HStack>
      <HStack gap="2" wrap="wrap" fontSize="sm" color="fg.muted" minW="0">
        {b.proposer && (
          <HStack gap="1.5" minW="0" maxW="full">
            <Text as="span">by</Text>
            <ValidatorChip operator={b.proposer.operator_address} moniker={b.proposer.moniker} />
          </HStack>
        )}
        <Text as="span">
          {formatCount(b.tx_count)} transaction{b.tx_count === 1 ? "" : "s"}
        </Text>
      </HStack>
    </Row>
  );
}

function CounterpartyChip({ c, ctx }: { c: Counterparty; ctx: RenderContext }) {
  if (c.kind === "account") return <AccountChip address={c.address} />;
  if (c.kind === "validator") return <ValidatorChip operator={c.operator} moniker={ctx.validator(c.operator)} />;
  return (
    <Text as="span" fontSize="sm">
      {c.name}
    </Text>
  );
}

function TxRow({ tx, ctx }: { tx: TxSummary; ctx: RenderContext }) {
  const other = txCounterparty(tx, ctx);
  return (
    <Row testId="latest-tx">
      <HStack justify="space-between" gap="3" align="start">
        <Text fontSize="sm" fontWeight="medium" minW="0" data-testid="latest-tx-what">
          {txOneLine(tx, ctx)}
        </Text>
        <TimeAgo time={tx.time} fontSize="sm" color="fg.muted" flexShrink={0} />
      </HStack>
      <HStack gap="2" wrap="wrap" minW="0">
        <HashLink hash={tx.hash} noCopy />
        {tx.signer && (
          <HStack gap="1.5" minW="0" maxW="full" data-testid="latest-tx-parties">
            <AccountChip address={tx.signer} />
            {other && (
              <>
                <Text as="span" color="fg.muted" aria-label="to">
                  →
                </Text>
                <CounterpartyChip c={other} ctx={ctx} />
              </>
            )}
          </HStack>
        )}
      </HStack>
    </Row>
  );
}

function LatestBlocks() {
  const blocks = useBlocks({ limit: 5 });
  return (
    <Latest title="Latest blocks" href="/blocks" id="latest-blocks">
      {blocks.data ? (
        <Box {...panel} role="list" aria-label="Latest blocks">
          {blocks.data.items.slice(0, 5).map((b) => (
            <BlockRow key={b.height} b={b} />
          ))}
        </Box>
      ) : blocks.error ? (
        <Unavailable what="blocks" />
      ) : (
        <ListSkeleton rows={5} />
      )}
    </Latest>
  );
}

function LatestTransactions() {
  const txs = useTransactions({ limit: 5 });
  const ctx = useListRenderContext();
  return (
    <Latest title="Latest transactions" href="/txs" id="latest-txs">
      {txs.data ? (
        <Box {...panel} role="list" aria-label="Latest transactions">
          {txs.data.items.slice(0, 5).map((tx) => (
            <TxRow key={tx.hash} tx={tx} ctx={ctx} />
          ))}
        </Box>
      ) : txs.error ? (
        <Unavailable what="transactions" />
      ) : (
        <ListSkeleton rows={5} />
      )}
    </Latest>
  );
}

function GoTo() {
  return (
    <Stack as="section" gap="3" aria-labelledby="go-to">
      <Heading as="h2" size="lg" id="go-to">
        Go to
      </Heading>
      <SimpleGrid columns={{ base: 2, md: 4, lg: 7 }} gap="3">
        {appLinks.map((l) => (
          <Link
            key={l.label}
            href={l.href}
            {...panel}
            px="4"
            minH="56px"
            justifyContent="space-between"
            fontWeight="medium"
            _hover={{ bg: "bg.muted", textDecoration: "none" }}
          >
            {l.label}
            <LuExternalLink aria-hidden />
          </Link>
        ))}
      </SimpleGrid>
    </Stack>
  );
}

/** `/`: the search box, the network's numbers, the latest blocks and transactions, all live. */
export function HomePage() {
  return (
    <Stack gap={{ base: "8", md: "12" }}>
      <Stack as="section" gap="4" align="center" textAlign="center" py={{ base: "4", md: "8" }}>
        <Heading as="h1" size={{ base: "2xl", md: "4xl" }}>
          Explore the BeeZee network
        </Heading>
        <Text color="fg.muted" maxW="2xl">
          Look up any block, transaction, address or validator, and see in plain words what happened on chain.
        </Text>
        <Box w="full" maxW="2xl">
          <SearchBox size="lg" />
        </Box>
      </Stack>
      <Tiles />
      <SimpleGrid columns={{ base: 1, lg: 2 }} gap="6">
        <LatestBlocks />
        <LatestTransactions />
      </SimpleGrid>
      <GoTo />
    </Stack>
  );
}
