"use client";

import { Box, Button, HStack, IconButton, Link, Skeleton, Stack, Tabs, Text, type LinkProps } from "@chakra-ui/react";
import NextLink from "next/link";
import { useMemo } from "react";
import { LuChevronLeft, LuChevronRight, LuClock, LuTriangleAlert } from "react-icons/lu";

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
import { DetailList, DetailRow } from "@/components/ui/detail-list";
import { JsonBlock } from "@/components/ui/json-block";
import { HashLink } from "@/components/ui/links";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { MoreDetails } from "@/components/ui/more-details";
import { Breadcrumb, DetailMeta, DetailTitle, SectionHead } from "@/components/ui/page-head";
import { CardSkeleton, JsonSkeleton, TableSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { StatusPill, Tag } from "@/components/ui/status-pill";
import { TimeAgo } from "@/components/ui/time-ago";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount, formatDateTime, formatPct } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { blockFees, blockGas, blockNotFound, blockTimeText, type BlockNotFound } from "@/render/blocks";
import { txOneLine } from "@/render/txs";
import { coinsText } from "@/render/messages/common";
import { createRenderContext } from "@/render/messages/registry";
import type { RenderContext } from "@/render/messages/types";

import { BlockFlow, FlowSkeleton } from "./block-flow";

export function pingPubBlockUrl(height: number): string {
  return `${externalLinks.pingPub}/block/${height}`;
}

/** "Blocks / 25,120,440", "Block 25,120,440" and the square arrows to the previous and next blocks. */
function Head({ height }: { height: number }) {
  return (
    <>
      <Breadcrumb path={[{ label: "Blocks", href: "/blocks" }]} current={formatCount(height)} mono />
      <DetailTitle
        title={
          <>
            Block{" "}
            <Text as="span" fontFamily="mono">
              {formatCount(height)}
            </Text>
          </>
        }
      >
        <HStack gap="1">
          {height > 1 && <Arrow href={`/blocks/${height - 1}`} label="Previous block" icon={<LuChevronLeft size={14} />} />}
          <Arrow href={`/blocks/${height + 1}`} label="Next block" icon={<LuChevronRight size={14} />} />
        </HStack>
      </DetailTitle>
    </>
  );
}

function Arrow({ href, label, icon }: { href: string; label: string; icon: React.ReactNode }) {
  return (
    <IconButton
      asChild
      variant="outline"
      boxSize="28px"
      minW="28px"
      h="28px"
      rounded="l1"
      color="fg.muted"
      borderColor="border.panel"
      _hover={{ color: "accent.fg", borderColor: "accent.fg", bg: "transparent" }}
      aria-label={label}
    >
      <NextLink href={href}>{icon}</NextLink>
    </IconButton>
  );
}

function Header({ block, tip }: { block: Block; tip: number | null }) {
  const succeeded = block.tx_count - block.tx_failed_count;
  return (
    <Box>
      <Head height={block.height} />
      <DetailMeta data-testid="block-meta">
        <Text as="span" data-testid="block-when">
          <TimeAgo time={block.time} />
          {block.height === tip && " · latest block"}
        </Text>
        <HStack as="span" gap="10px" wrap="wrap" data-testid="block-chips">
          <Tag bg="transparent" borderWidth="1px" borderColor="accent.fg">
            {formatCount(block.tx_count)} transaction{block.tx_count === 1 ? "" : "s"}
          </Tag>
          {block.tx_count > 0 && <Tag tone="ok">{formatCount(succeeded)} succeeded</Tag>}
          {block.tx_failed_count > 0 && <Tag tone="bad">{formatCount(block.tx_failed_count)} failed</Tag>}
        </HStack>
      </DetailMeta>
    </Box>
  );
}

const Muted = ({ children }: { children: React.ReactNode }) => (
  <Text as="span" color="fg.muted">
    {children}
  </Text>
);

function Overview({ block, ctx }: { block: Block; ctx: RenderContext }) {
  const fees = blockFees(block);
  return (
    <Card title="Overview" p="0" mt="8" data-testid="block-overview">
      <DetailList>
        <DetailRow label="Time">
          <TimeAgo time={block.time} />
          <Muted>· {formatDateTime(block.time)}</Muted>
        </DetailRow>
        <DetailRow label="Proposed by">
          {block.proposer ? (
            <ValidatorChip operator={block.proposer.operator_address} moniker={block.proposer.moniker} />
          ) : (
            <Muted>unknown</Muted>
          )}
        </DetailRow>
        <DetailRow label="Fees">
          {fees.coins.length > 0 ? (
            <>
              <Text as="span" fontFamily="mono">
                {coinsText(fees.coins, ctx)}
              </Text>
              <Muted>
                · collected from {formatCount(fees.payers)} transaction{fees.payers === 1 ? "" : "s"}
              </Muted>
            </>
          ) : (
            <Muted>none</Muted>
          )}
        </DetailRow>
        <DetailRow label="Block time">
          <Text as="span" fontFamily="mono">
            {blockTimeText(block.block_time_ms)}
          </Text>
          {block.block_time_ms != null && block.height > 1 && (
            <>
              <Muted>· since block</Muted>
              <Link asChild fontFamily="mono" color="accent.fg">
                <NextLink href={`/blocks/${block.height - 1}`}>{formatCount(block.height - 1)}</NextLink>
              </Link>
            </>
          )}
        </DetailRow>
        <DetailRow label="Hash" last>
          <Text as="span" fontFamily="mono" fontSize="12px" wordBreak="break-all" data-testid="block-hash">
            {block.hash}
          </Text>
          <CopyButton value={block.hash} label="Copy hash" />
        </DetailRow>
      </DetailList>
    </Card>
  );
}

// Hash (c2) · What happened · By (c2) · Status · Fee (c3).
const TX_COLUMNS = {
  base: "minmax(0, 1fr) auto",
  c2: "110px minmax(0, 2fr) minmax(0, 1fr) auto",
  c3: "110px minmax(0, 2fr) minmax(0, 1fr) auto minmax(0, 0.8fr)",
};

function TxRow({ tx, ctx }: { tx: BlockTx; ctx: RenderContext }) {
  return (
    <ListRow role="listitem" templateColumns={TX_COLUMNS} data-testid="block-tx">
      <Box hideBelow="c2" minW="0">
        <HashLink hash={tx.hash} noCopy />
      </Box>
      <Link asChild color="fg" minW="0" _hover={{ color: "accent.fg" }}>
        <NextLink href={`/txs/${tx.hash}`} data-testid="block-tx-what">
          {txOneLine(tx, ctx)}
        </NextLink>
      </Link>
      <Box hideBelow="c2" minW="0">
        {tx.signer ? <AccountChip address={tx.signer} label={ctx.label(tx.signer)} /> : <Muted>—</Muted>}
      </Box>
      <StatusPill success={tx.success} />
      <Text hideBelow="c3" textAlign="right" fontFamily="mono" color="fg.muted" truncate>
        {tx.fee.length ? coinsText(tx.fee, ctx) : "none"}
      </Text>
    </ListRow>
  );
}

function Transactions({ block, ctx }: { block: Block; ctx: RenderContext }) {
  return (
    <Box as="section" mt="8" aria-labelledby="block-txs-title">
      <SectionHead id="block-txs-title" title="Transactions in this block" />
      <ListPanel>
        {block.transactions.length === 0 ? (
          <Text px="4" py="6" color="fg.muted" fontSize="sm">
            No transactions in this block
          </Text>
        ) : (
          <>
            <ListHead templateColumns={TX_COLUMNS} aria-hidden>
              <Text hideBelow="c2">Hash</Text>
              <Text>What happened</Text>
              <Text hideBelow="c2">By</Text>
              <Text>Status</Text>
              <Text hideBelow="c3" textAlign="right">
                Fee
              </Text>
            </ListHead>
            <Box role="list" aria-label="Transactions">
              {block.transactions.map((tx) => (
                <TxRow key={tx.hash} tx={tx} ctx={ctx} />
              ))}
            </Box>
          </>
        )}
      </ListPanel>
    </Box>
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

/** "Open on Ping.pub ↗": on the tab bar from c2 up, under the JSON on phones where the tabs fill the bar. */
function PingPubOpen({ height, ...props }: { height: number } & Omit<LinkProps, "href" | "children">) {
  return (
    <Link href={pingPubBlockUrl(height)} target="_blank" rel="noopener noreferrer" fontSize="12px" color="accent.fg" whiteSpace="nowrap" {...props}>
      Open on Ping.pub ↗
    </Link>
  );
}

/** The content of "More details": gas, signatures, the raw JSON tabs and Ping.pub. */
function BlockDetails({ block }: { block: Block }) {
  const gas = blockGas(block);
  return (
    <ListPanel>
      <DetailList>
        <DetailRow label="Gas">
          {gas != null ? (
            <>
              <Text as="span" fontFamily="mono">
                {formatCount(gas)}
              </Text>
              <Muted>used in this block</Muted>
            </>
          ) : (
            <Muted>unknown</Muted>
          )}
        </DetailRow>
        <DetailRow label="Signatures" last>
          <Signatures block={block} />
        </DetailRow>
      </DetailList>
      <Tabs.Root defaultValue="block" variant="line" lazyMount>
        <Tabs.List px="2" borderTopWidth="1px" borderColor="border.panel" alignItems="center">
          <Tabs.Trigger value="block" fontSize="13px" px="2.5" whiteSpace="nowrap">
            Raw block
          </Tabs.Trigger>
          <Tabs.Trigger value="results" fontSize="13px" px="2.5" whiteSpace="nowrap">
            Block results
          </Tabs.Trigger>
          <Tabs.Trigger value="commit" fontSize="13px" px="2.5" whiteSpace="nowrap">
            Commit
          </Tabs.Trigger>
          <Box flex="1" />
          <PingPubOpen height={block.height} hideBelow="c2" px="2" />
        </Tabs.List>
        <Tabs.Content value="block" p="4" pt="3">
          <RawBlockTab height={block.height} />
        </Tabs.Content>
        <Tabs.Content value="results" p="4" pt="3">
          <RawResultsTab height={block.height} />
        </Tabs.Content>
        <Tabs.Content value="commit" p="4" pt="3">
          <RawCommitTab height={block.height} />
        </Tabs.Content>
      </Tabs.Root>
      <PingPubOpen height={block.height} hideFrom="c2" display="inline-block" px="4" pb="4" />
    </ListPanel>
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
    <Box>
      <Header block={block} tip={tip} />
      <Overview block={block} ctx={ctx} />
      <Transactions block={block} ctx={ctx} />
      {block.transfers_count > 0 && <BlockFlow height={block.height} count={block.transfers_count} />}
      <Box mt="8">
        <MoreDetails hint="gas, signatures, raw JSON">
          <BlockDetails block={block} />
        </MoreDetails>
      </Box>
    </Box>
  );
}

/** The page's skeleton: the header, the overview grid, five transaction rows and the funds-flow group lines. */
export function BlockSkeleton() {
  return (
    <Stack gap="8" pt="6" aria-busy data-testid="block-skeleton">
      <Stack gap="3">
        <Skeleton h="3" w="40" />
        <Skeleton h="8" w="72" maxW="full" />
        <HStack gap="2">
          <Skeleton h="4" w="24" />
          <Skeleton h="5" w="28" />
          <Skeleton h="5" w="24" />
        </HStack>
      </Stack>
      <CardSkeleton lines={5} />
      <TableSkeleton rows={5} columns={5} />
      <FlowSkeleton />
    </Stack>
  );
}

/** Two rails with a dashed gap: a height missing from the index. */
function GapIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden>
      <path d="M4 6h16M4 18h16" />
      <path d="M9 12h6" strokeDasharray="2 2" />
    </svg>
  );
}

function PingPubLookup({ height }: { height: number }) {
  return (
    <Link href={pingPubBlockUrl(height)} target="_blank" rel="noopener noreferrer" color="accent.fg">
      Look it up on Ping.pub ↗
    </Link>
  );
}

function NotFoundBox({ height, kind, oldest, expired }: { height: number; kind: BlockNotFound; oldest: number | null; expired: boolean }) {
  if (kind === "too_old") {
    return (
      <StateBox
        icon={<LuClock size={20} />}
        title={
          <>
            Older than the explorer&apos;s history
            {oldest != null && (
              <Text as="span" color="fg.muted" fontFamily="mono" fontWeight="400">
                {" "}
                (from block {formatCount(oldest)})
              </Text>
            )}
          </>
        }
        actions={<PingPubLookup height={height} />}
        data-testid="block-too_old"
      >
        The block is on the chain, the explorer just doesn&apos;t keep blocks this old.
      </StateBox>
    );
  }
  if (kind === "missing") {
    return (
      <StateBox
        tone="warn"
        icon={<GapIcon />}
        title="Not indexed: this height is missing from the explorer"
        actions={<PingPubLookup height={height} />}
        data-testid="block-missing"
      />
    );
  }
  // Not produced yet: unchanged until the Future block page replaces it.
  return (
    <Card data-testid="block-not_produced">
      <Stack gap="2" role={expired ? undefined : "status"}>
        <Text fontWeight="semibold">Not produced yet</Text>
        <Text color="fg.muted" fontSize="sm">
          {expired
            ? "The chain has not reached this height yet. Reload the page to look again."
            : "The chain has not reached this height yet. This page shows it as soon as it is produced."}
        </Text>
      </Stack>
    </Card>
  );
}

function NotFound(props: { height: number; kind: BlockNotFound; oldest: number | null; expired: boolean }) {
  return (
    <Box>
      <Head height={props.height} />
      <Box mt="6">
        <NotFoundBox {...props} />
      </Box>
    </Box>
  );
}

function LoadError({ height, onRetry }: { height: number; onRetry: () => void }) {
  return (
    <Box>
      <Head height={height} />
      <StateBox
        mt="6"
        tone="error"
        icon={<LuTriangleAlert size={20} />}
        title="This block could not be loaded"
        actions={
          <Button size="sm" colorPalette="blue" onClick={onRetry}>
            Try again
          </Button>
        }
        data-testid="block-error"
      >
        The explorer did not answer. Try again in a moment.
      </StateBox>
    </Box>
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
  if (error && !isNotFound(error)) return <LoadError height={height} onRetry={() => void refetch()} />;
  return <BlockSkeleton />;
}
