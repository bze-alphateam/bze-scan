"use client";

import { Badge, Box, Button, Grid, Heading, HStack, IconButton, Link, Skeleton, Stack, Tabs, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useMemo } from "react";
import { LuChevronLeft, LuChevronRight, LuExternalLink } from "react-icons/lu";

import { isNotFound } from "@/api/client";
import {
  useBlock,
  useRawBlock,
  useRawBlockResults,
  useRawCommit,
  useStats,
  useStatus,
  useTokenDirectory,
  useValidatorDirectory,
} from "@/api/hooks";
import type { Block, BlockTx, RawRpcResponse } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { JsonBlock } from "@/components/ui/json-block";
import { HashLink } from "@/components/ui/links";
import { MoreDetails } from "@/components/ui/more-details";
import { CardSkeleton, JsonSkeleton, TableSkeleton } from "@/components/ui/skeleton";
import { StatusPill } from "@/components/ui/status-pill";
import { TimeAgo } from "@/components/ui/time-ago";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount, formatDateTime, formatPct } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { blockFees, blockGas, blockNotFound, blockTimeText, txOneLine, type BlockNotFound } from "@/render/blocks";
import { coinsText } from "@/render/messages/common";
import { createRenderContext } from "@/render/messages/registry";
import type { RenderContext } from "@/render/messages/types";

import { BlockFlow, FlowSkeleton } from "./block-flow";

export function pingPubBlockUrl(height: number): string {
  return `${externalLinks.pingPub}/block/${height}`;
}

/** "Blocks / 25,120,440". */
function Breadcrumb({ height }: { height: number }) {
  return (
    <HStack as="nav" aria-label="Breadcrumb" gap="2" fontSize="sm" color="fg.muted">
      <Link asChild color="fg.muted">
        <NextLink href="/blocks">Blocks</NextLink>
      </Link>
      <Text as="span" aria-hidden>
        /
      </Text>
      <Text as="span" aria-current="page">
        {formatCount(height)}
      </Text>
    </HStack>
  );
}

/** "Block N" with the previous and next blocks' arrows. */
function Title({ height }: { height: number }) {
  return (
    <HStack gap="2" wrap="wrap">
      <Heading as="h1" size={{ base: "2xl", md: "3xl" }}>
        Block {formatCount(height)}
      </Heading>
      <HStack gap="1">
        {height > 1 && (
          <IconButton asChild variant="ghost" size="sm" minW="44px" minH="44px" aria-label="Previous block">
            <NextLink href={`/blocks/${height - 1}`}>
              <LuChevronLeft />
            </NextLink>
          </IconButton>
        )}
        <IconButton asChild variant="ghost" size="sm" minW="44px" minH="44px" aria-label="Next block">
          <NextLink href={`/blocks/${height + 1}`}>
            <LuChevronRight />
          </NextLink>
        </IconButton>
      </HStack>
    </HStack>
  );
}

function Header({ block, tip }: { block: Block; tip: number | null }) {
  const succeeded = block.tx_count - block.tx_failed_count;
  return (
    <Stack gap="3">
      <Breadcrumb height={block.height} />
      <Title height={block.height} />
      <Text color="fg.muted" fontSize="sm" data-testid="block-when">
        <TimeAgo time={block.time} />
        {block.height === tip && " · latest block"}
      </Text>
      <HStack gap="2" wrap="wrap" data-testid="block-chips">
        <Badge variant="outline" size="lg" rounded="full">
          {formatCount(block.tx_count)} transaction{block.tx_count === 1 ? "" : "s"}
        </Badge>
        {block.tx_count > 0 && (
          <Badge colorPalette="green" variant="subtle" size="lg" rounded="full">
            {formatCount(succeeded)} succeeded
          </Badge>
        )}
        {block.tx_failed_count > 0 && (
          <Badge colorPalette="red" variant="subtle" size="lg" rounded="full">
            {formatCount(block.tx_failed_count)} failed
          </Badge>
        )}
      </HStack>
    </Stack>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <>
      <Text as="dt" fontSize="sm" color="fg.muted" mt={{ base: "2", sm: "0" }}>
        {label}
      </Text>
      <Box as="dd" fontSize="sm" minW="0">
        {children}
      </Box>
    </>
  );
}

const DL = { base: "1fr", sm: "minmax(8rem, max-content) 1fr" };

function Overview({ block, ctx }: { block: Block; ctx: RenderContext }) {
  const fees = blockFees(block);
  return (
    <Card title="Overview" data-testid="block-overview">
      <Grid as="dl" templateColumns={DL} columnGap="6" rowGap={{ base: "1", sm: "3" }}>
        <Row label="Time">
          <TimeAgo time={block.time} />
          <Text as="span" color="fg.muted">
            {" "}
            · {formatDateTime(block.time)}
          </Text>
        </Row>
        <Row label="Proposed by">
          {block.proposer ? (
            <ValidatorChip operator={block.proposer.operator_address} moniker={block.proposer.moniker} />
          ) : (
            <Text as="span" color="fg.muted">
              unknown
            </Text>
          )}
        </Row>
        <Row label="Fees">
          {fees.coins.length > 0 ? (
            <>
              {coinsText(fees.coins, ctx)}
              <Text as="span" color="fg.muted">
                {" "}
                · collected from {formatCount(fees.payers)} transaction{fees.payers === 1 ? "" : "s"}
              </Text>
            </>
          ) : (
            <Text as="span" color="fg.muted">
              none
            </Text>
          )}
        </Row>
        <Row label="Block time">
          {blockTimeText(block.block_time_ms)}
          {block.block_time_ms != null && block.height > 1 && (
            <Text as="span" color="fg.muted">
              {" "}
              · since block{" "}
              <Link asChild>
                <NextLink href={`/blocks/${block.height - 1}`}>{formatCount(block.height - 1)}</NextLink>
              </Link>
            </Text>
          )}
        </Row>
        <Row label="Hash">
          <HStack gap="1" align="start">
            <Text as="span" fontFamily="mono" wordBreak="break-all" data-testid="block-hash">
              {block.hash}
            </Text>
            <CopyButton value={block.hash} label="Copy hash" />
          </HStack>
        </Row>
      </Grid>
    </Card>
  );
}

const TX_COLUMNS = { base: "minmax(0, 1fr) auto", md: "110px minmax(0, 2fr) minmax(0, 1fr) 104px minmax(0, 0.8fr)" };

function PhoneLabel({ children }: { children: string }) {
  return (
    <Text as="span" hideFrom="md" fontSize="xs" color="fg.muted" me="1">
      {children}
    </Text>
  );
}

function TxRow({ tx, ctx }: { tx: BlockTx; ctx: RenderContext }) {
  return (
    <Grid
      role="listitem"
      templateColumns={TX_COLUMNS}
      gap={{ base: "1", md: "4" }}
      alignItems="center"
      px="4"
      py="3"
      minH="56px"
      borderTopWidth="1px"
      borderColor="border.panel"
      data-testid="block-tx"
    >
      <HashLink hash={tx.hash} noCopy />
      <Box gridRow={{ base: "1", md: "auto" }} gridColumn={{ base: "2", md: "4" }} justifySelf={{ base: "end", md: "start" }}>
        <StatusPill success={tx.success} size="sm" />
      </Box>
      <Text fontSize="sm" gridColumn={{ base: "1 / -1", md: "2" }} gridRow={{ md: "1" }} data-testid="block-tx-what">
        {txOneLine(tx, ctx)}
      </Text>
      <Box minW="0" gridColumn={{ base: "1 / -1", md: "3" }} gridRow={{ md: "1" }}>
        <PhoneLabel>By</PhoneLabel>
        {tx.signer ? (
          <AccountChip address={tx.signer} label={ctx.label(tx.signer)} />
        ) : (
          <Text as="span" color="fg.muted">
            —
          </Text>
        )}
      </Box>
      <Text fontSize="sm" color="fg.muted" gridColumn={{ base: "1 / -1", md: "5" }} gridRow={{ md: "1" }}>
        <PhoneLabel>Fee</PhoneLabel>
        {tx.fee.length ? coinsText(tx.fee, ctx) : "none"}
      </Text>
    </Grid>
  );
}

function Transactions({ block, ctx }: { block: Block; ctx: RenderContext }) {
  return (
    <Stack as="section" gap="3" aria-labelledby="block-txs-title">
      <Heading as="h2" size="lg" id="block-txs-title">
        Transactions in this block
      </Heading>
      <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" overflow="hidden">
        {block.transactions.length === 0 ? (
          <Text px="4" py="6" color="fg.muted" fontSize="sm">
            No transactions in this block
          </Text>
        ) : (
          <>
            <Grid
              hideBelow="md"
              templateColumns={TX_COLUMNS}
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
              <Text>Hash</Text>
              <Text>What happened</Text>
              <Text>By</Text>
              <Text>Status</Text>
              <Text>Fee</Text>
            </Grid>
            <Box role="list" aria-label="Transactions">
              {block.transactions.map((tx) => (
                <TxRow key={tx.hash} tx={tx} ctx={ctx} />
              ))}
            </Box>
          </>
        )}
      </Box>
    </Stack>
  );
}

function RawJson({ query, what }: { query: { data?: RawRpcResponse; isError: boolean }; what: string }) {
  if (query.isError) {
    return (
      <Text fontSize="sm" color="fg.muted">
        The {what} could not be loaded from the archive nodes. Try again later, or open it on Ping.pub.
      </Text>
    );
  }
  return query.data ? <JsonBlock value={query.data} /> : <JsonSkeleton />;
}

function RawBlockTab({ height }: { height: number }) {
  return <RawJson query={useRawBlock(height)} what="raw block" />;
}

function RawResultsTab({ height }: { height: number }) {
  return <RawJson query={useRawBlockResults(height)} what="block results" />;
}

function RawCommitTab({ height }: { height: number }) {
  return <RawJson query={useRawCommit(height)} what="commit" />;
}

/** "5 of 7 validators · 93.4% of voting power". */
function Signatures({ block }: { block: Block }) {
  const stats = useStats();
  const active = stats.data?.validators?.active;
  if (block.signatures_count == null) return <Text as="span">unknown</Text>;
  return (
    <Text as="span">
      {formatCount(block.signatures_count)}
      {active != null && ` of ${formatCount(active)}`} validators
      {block.signatures_power_pct != null && ` · ${formatPct(block.signatures_power_pct)} of voting power`}
    </Text>
  );
}

/** The content of "More details": gas, signatures, the raw JSON tabs and Ping.pub. */
function BlockDetails({ block }: { block: Block }) {
  const gas = blockGas(block);
  return (
    <Stack gap="5" bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" p={{ base: "4", md: "5" }}>
      <Grid as="dl" templateColumns={DL} columnGap="6" rowGap={{ base: "1", sm: "3" }}>
        <Row label="Gas">{gas != null ? `${formatCount(gas)} used in this block` : "unknown"}</Row>
        <Row label="Signatures">
          <Signatures block={block} />
        </Row>
      </Grid>
      <Tabs.Root defaultValue="block" variant="line" lazyMount>
        <Tabs.List>
          <Tabs.Trigger value="block" minH="44px">
            Raw block
          </Tabs.Trigger>
          <Tabs.Trigger value="results" minH="44px">
            Block results
          </Tabs.Trigger>
          <Tabs.Trigger value="commit" minH="44px">
            Commit
          </Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="block" pt="3">
          <RawBlockTab height={block.height} />
        </Tabs.Content>
        <Tabs.Content value="results" pt="3">
          <RawResultsTab height={block.height} />
        </Tabs.Content>
        <Tabs.Content value="commit" pt="3">
          <RawCommitTab height={block.height} />
        </Tabs.Content>
      </Tabs.Root>
      <Link href={pingPubBlockUrl(block.height)} target="_blank" rel="noopener noreferrer" fontSize="sm" alignSelf="start">
        Open on Ping.pub <LuExternalLink aria-hidden />
      </Link>
    </Stack>
  );
}

/** A found block. */
export function BlockView({ block, tip }: { block: Block; tip: number | null }) {
  const tokens = useTokenDirectory();
  const validators = useValidatorDirectory();
  const ctx = useMemo(
    () => createRenderContext({ transfers: block.transfers, tokens: tokens.data, validators: validators.data }),
    [block.transfers, tokens.data, validators.data],
  );
  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Header block={block} tip={tip} />
      <Overview block={block} ctx={ctx} />
      <Transactions block={block} ctx={ctx} />
      {block.transfers_count > 0 && <BlockFlow height={block.height} count={block.transfers_count} />}
      <MoreDetails hint="gas, signatures, raw JSON">
        <BlockDetails block={block} />
      </MoreDetails>
    </Stack>
  );
}

/** The page's skeleton: the header, the overview grid, five transaction rows and the funds-flow group lines. */
export function BlockSkeleton() {
  return (
    <Stack gap={{ base: "6", md: "8" }} aria-busy data-testid="block-skeleton">
      <Stack gap="3">
        <Skeleton h="4" w="40" />
        <Skeleton h="9" w="72" maxW="full" />
        <Skeleton h="4" w="32" />
        <HStack gap="2">
          <Skeleton h="7" w="32" rounded="full" />
          <Skeleton h="7" w="28" rounded="full" />
        </HStack>
      </Stack>
      <CardSkeleton lines={5} />
      <TableSkeleton rows={5} columns={5} />
      <FlowSkeleton />
    </Stack>
  );
}

const NOT_FOUND_TEXT: Record<BlockNotFound, string> = {
  not_produced: "Not produced yet",
  too_old: "Older than the explorer's history",
  missing: "Not indexed: this height is missing from the explorer",
};

function NotFound({ height, kind, oldest, expired }: { height: number; kind: BlockNotFound; oldest: number | null; expired: boolean }) {
  return (
    <Stack gap="6">
      <Stack gap="3">
        <Breadcrumb height={height} />
        <Title height={height} />
      </Stack>
      <Card data-testid={`block-${kind}`}>
        <Stack gap="2" role={kind === "not_produced" && !expired ? "status" : undefined}>
          <Text fontWeight="semibold">
            {NOT_FOUND_TEXT[kind]}
            {kind === "too_old" && oldest != null && ` (from block ${formatCount(oldest)})`}
          </Text>
          {kind === "not_produced" ? (
            <Text color="fg.muted" fontSize="sm">
              {expired
                ? "The chain has not reached this height yet. Reload the page to look again."
                : "The chain has not reached this height yet. This page shows it as soon as it is produced."}
            </Text>
          ) : (
            <Link href={pingPubBlockUrl(height)} target="_blank" rel="noopener noreferrer" fontSize="sm" alignSelf="start">
              Look it up on Ping.pub <LuExternalLink aria-hidden />
            </Link>
          )}
        </Stack>
      </Card>
    </Stack>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <Card title="This block could not be loaded">
      <HStack justify="space-between" wrap="wrap" gap="3">
        <Text color="fg.muted" fontSize="sm">
          The explorer did not answer. Try again in a moment.
        </Text>
        <Button colorPalette="blue" minH="44px" onClick={onRetry}>
          Try again
        </Button>
      </HStack>
    </Card>
  );
}

/**
 * `/blocks/[height]`: the skeleton, then the block, or why the explorer has
 * none (a height above its latest block is asked for again for two minutes).
 * Mount under a `key` of the height.
 */
export function BlockPage({ height }: { height: number }) {
  const status = useStatus();
  const tip = status.data?.live_fill.db_height ?? null;
  const { data, error, expired, refetch } = useBlock(height, { waitForIt: tip != null && height > tip });
  if (data) return <BlockView block={data} tip={tip} />;
  if (isNotFound(error) && (status.data || status.error)) {
    return (
      <NotFound
        height={height}
        kind={blockNotFound(height, status.data)}
        oldest={status.data?.back_fill.oldest_height ?? null}
        expired={expired}
      />
    );
  }
  if (error && !isNotFound(error)) return <LoadError onRetry={() => void refetch()} />;
  return <BlockSkeleton />;
}
