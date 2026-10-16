"use client";

import { Box, Heading, HStack, Link, SimpleGrid, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import type { ReactNode } from "react";
import { LuExternalLink } from "react-icons/lu";

import { useBlocks, useTransactions } from "@/api/hooks";
import type { BlockSummary, TxSummary } from "@/api/types";
import { AppIcon } from "@/components/layout/app-icon";
import { SearchBox } from "@/components/layout/search-box";
import { useListRenderContext } from "@/components/txs/list-context";
import { AccountChip } from "@/components/ui/account-chip";
import { HashLink } from "@/components/ui/links";
import { ListPanel } from "@/components/ui/list";
import { SectionHead } from "@/components/ui/page-head";
import { ListSkeleton } from "@/components/ui/skeleton";
import { TimeAgo } from "@/components/ui/time-ago";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount } from "@/lib/format";
import { appAccent, type AppLink, appLinks } from "@/lib/links";
import type { Counterparty, RenderContext } from "@/render/messages/types";
import { txCounterparty, txOneLine } from "@/render/txs";

/** A home list: its head with "View all →" and its rows. */
function Latest({ title, href, id, children }: { title: string; href: string; id: string; children: ReactNode }) {
  return (
    <Box as="section" minW="0" aria-labelledby={id} data-testid={id}>
      <SectionHead id={id} title={title}>
        <Link asChild fontSize="12px" color="accent.fg">
          <NextLink href={href} aria-label={`View all ${title.replace(/^Latest /, "")}`}>
            View all →
          </NextLink>
        </Link>
      </SectionHead>
      {children}
    </Box>
  );
}

function Unavailable({ what }: { what: string }) {
  return (
    <ListPanel p="4">
      <Text fontSize="sm" color="fg.muted">
        The latest {what} could not be loaded. They will appear as soon as the explorer answers.
      </Text>
    </ListPanel>
  );
}

/** One row: the top line (link and age) over the muted meta line. */
function Row({ top, meta, testId }: { top: ReactNode; meta: ReactNode; testId: string }) {
  return (
    <Box
      role="listitem"
      px="4"
      py="3"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      _hover={{ bg: "bg.hover" }}
      data-testid={testId}
    >
      <HStack justify="space-between" gap="4" align="baseline">
        {top}
      </HStack>
      <HStack gap="2" wrap="wrap" mt="6px" fontSize="12px" color="fg.muted" minW="0">
        {meta}
      </HStack>
    </Box>
  );
}

const Age = ({ time }: { time: string }) => (
  <TimeAgo time={time} fontFamily="mono" fontSize="12px" color="fg.muted" whiteSpace="nowrap" flexShrink={0} />
);

function BlockRow({ b }: { b: BlockSummary }) {
  return (
    <Row
      testId="latest-block"
      top={
        <>
          <Link asChild fontFamily="mono" fontWeight="500" color="accent.fg">
            <NextLink href={`/blocks/${b.height}`}>{formatCount(b.height)}</NextLink>
          </Link>
          <Age time={b.time} />
        </>
      }
      meta={
        <>
          {b.proposer && (
            <>
              <Text as="span">by</Text>
              <ValidatorChip operator={b.proposer.operator_address} moniker={b.proposer.moniker} minW="0" />
              <Text as="span" aria-hidden>
                ·
              </Text>
            </>
          )}
          <Text as="span">
            <Text as="span" fontFamily="mono">
              {formatCount(b.tx_count)}
            </Text>{" "}
            transaction{b.tx_count === 1 ? "" : "s"}
          </Text>
        </>
      }
    />
  );
}

/** A module account by name, dashed (the DEX, the Burner). */
function ModuleChip({ name }: { name: string }) {
  return (
    <Box
      as="span"
      display="inline-flex"
      alignItems="center"
      h="20px"
      px="7px"
      borderWidth="1px"
      borderStyle="dashed"
      borderColor="fg.muted"
      rounded="l1"
      color="fg.muted"
      fontSize="11px"
      fontWeight="600"
      letterSpacing="0.04em"
      whiteSpace="nowrap"
      data-module
    >
      {name}
    </Box>
  );
}

function CounterpartyChip({ c, ctx }: { c: Counterparty; ctx: RenderContext }) {
  if (c.kind === "account") return <AccountChip address={c.address} label={ctx.label(c.address)} />;
  if (c.kind === "validator") return <ValidatorChip operator={c.operator} moniker={ctx.validator(c.operator)} />;
  return <ModuleChip name={c.name} />;
}

function TxRow({ tx, ctx }: { tx: TxSummary; ctx: RenderContext }) {
  const other = txCounterparty(tx, ctx);
  return (
    <Row
      testId="latest-tx"
      top={
        <>
          <Link asChild color="fg" fontWeight="500" minW="0" _hover={{ color: "accent.fg" }}>
            <NextLink href={`/txs/${tx.hash}`} data-testid="latest-tx-what">
              {txOneLine(tx, ctx)}
            </NextLink>
          </Link>
          <Age time={tx.time} />
        </>
      }
      meta={
        <>
          <HashLink hash={tx.hash} noCopy fontSize="12px" />
          {tx.signer && (
            <HStack gap="2" minW="0" maxW="full" data-testid="latest-tx-parties">
              <AccountChip address={tx.signer} label={ctx.label(tx.signer)} />
              {other && (
                <>
                  <Text as="span" aria-label="to">
                    →
                  </Text>
                  <CounterpartyChip c={other} ctx={ctx} />
                </>
              )}
            </HStack>
          )}
        </>
      }
    />
  );
}

function LatestBlocks() {
  const blocks = useBlocks({ limit: 5 });
  return (
    <Latest title="Latest blocks" href="/blocks" id="latest-blocks">
      {blocks.data ? (
        <ListPanel role="list" aria-label="Latest blocks">
          {blocks.data.items.slice(0, 5).map((b) => (
            <BlockRow key={b.height} b={b} />
          ))}
        </ListPanel>
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
        <ListPanel role="list" aria-label="Latest transactions">
          {txs.data.items.slice(0, 5).map((tx) => (
            <TxRow key={tx.hash} tx={tx} ctx={ctx} />
          ))}
        </ListPanel>
      ) : txs.error ? (
        <Unavailable what="transactions" />
      ) : (
        <ListSkeleton rows={5} />
      )}
    </Latest>
  );
}

/**
 * One "Go to" tile: the app's icon and name; on hover, the app's accent
 * (Website and Docs keep the explorer's). The spacing is set so that
 * "Communities" (85 px) with its icon, arrow, padding and borders (155 px)
 * fits a 158 px tile, the size on a 360 px phone.
 */
function GoToTile({ link }: { link: AppLink }) {
  const accent = appAccent(link.icon);
  const hover = accent ? { base: accent.light, _dark: accent.dark } : "accent.fg";
  return (
    <Link
      href={link.href}
      display="flex"
      justifyContent="space-between"
      alignItems="center"
      gap="1"
      px="2"
      py="10px"
      bg="bg.panel"
      borderWidth="1px"
      borderColor="border.panel"
      rounded="l3"
      color="fg"
      fontWeight="500"
      _hover={{ textDecoration: "none", borderColor: hover, color: hover }}
    >
      <HStack as="span" gap="1.5" minW="0">
        <AppIcon app={link.icon} size={28} />
        <Text as="span" truncate data-testid="go-to-name">
          {link.label}
        </Text>
      </HStack>
      <LuExternalLink size={14} aria-hidden style={{ flexShrink: 0 }} />
    </Link>
  );
}

function GoTo() {
  return (
    <Box as="section" aria-labelledby="go-to" mt="10">
      <SectionHead id="go-to" title="Go to" />
      <SimpleGrid as="nav" aria-label="BZE apps" minChildWidth="158px" gap="3">
        {appLinks.map((l) => (
          <GoToTile key={l.label} link={l} />
        ))}
      </SimpleGrid>
    </Box>
  );
}

/** `/`: the search box and the latest blocks and transactions, all live; the network strip above carries the figures. */
export function HomePage() {
  return (
    <Box>
      <Stack as="section" gap="0" align="center" textAlign="center" pt={{ base: "40px", md: "64px" }} pb={{ base: "32px", md: "52px" }}>
        <Heading as="h1" fontSize={{ base: "26px", md: "34px" }} fontWeight="600" letterSpacing="-0.01em" lineHeight="1.2">
          Explore the BeeZee network
        </Heading>
        <Text color="fg.muted" maxW="560px" mt="3" fontSize="15px" lineHeight="1.55">
          Look up any block, transaction, address or validator, and see in plain words what happened on chain.
        </Text>
        <Box w="full" maxW="620px" mt="7">
          <SearchBox size="lg" />
        </Box>
      </Stack>
      <SimpleGrid columns={{ base: 1, lg: 2 }} gap="6" mt="2">
        <LatestBlocks />
        <LatestTransactions />
      </SimpleGrid>
      <GoTo />
    </Box>
  );
}
