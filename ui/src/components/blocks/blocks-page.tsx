"use client";

import { Box, Button, HStack, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { LuClock, LuTriangleAlert } from "react-icons/lu";

import { useBlocks, useStats, useStatus } from "@/api/hooks";
import type { BlockSummary } from "@/api/types";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { PageTitle } from "@/components/ui/page-head";
import { Pager, PagerPill } from "@/components/ui/pager";
import { TableSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { TimeAgo } from "@/components/ui/time-ago";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount } from "@/lib/format";
import {
  BLOCKS_PER_PAGE,
  blocksHref,
  blocksLastPage,
  blocksPageBefore,
  blockTimeText,
  sizeText,
  type BlocksPlace,
} from "@/render/blocks";

// Height · Time (c2) · Proposer · Transactions · Block time (c3) · Size (c3).
const COLUMNS = {
  base: "minmax(96px, auto) minmax(0, 1fr) auto",
  c2: "120px 140px minmax(0, 1fr) 120px",
  c3: "120px 140px minmax(0, 1fr) 120px 100px 90px",
};

/** "7" and "· 1 failed" in red when some failed; muted when the block is empty. */
export function TxCount({ b }: { b: Pick<BlockSummary, "tx_count" | "tx_failed_count"> }) {
  return (
    <Text as="span" color={b.tx_count === 0 ? "fg.muted" : undefined}>
      {formatCount(b.tx_count)}
      {b.tx_failed_count > 0 && (
        <Text as="span" color="neg.fg">
          {" "}
          · {formatCount(b.tx_failed_count)} failed
        </Text>
      )}
    </Text>
  );
}

function BlockRow({ b }: { b: BlockSummary }) {
  return (
    <ListRow role="listitem" templateColumns={COLUMNS} data-testid="block-row">
      <Link asChild fontFamily="mono" fontWeight="500" color="accent.fg">
        <NextLink href={`/blocks/${b.height}`}>{formatCount(b.height)}</NextLink>
      </Link>
      <TimeAgo time={b.time} hideBelow="c2" fontSize="13px" color="fg.muted" whiteSpace="nowrap" />
      <Box minW="0">
        {b.proposer ? (
          <ValidatorChip operator={b.proposer.operator_address} moniker={b.proposer.moniker} />
        ) : (
          <Text as="span" color="fg.muted">
            —
          </Text>
        )}
      </Box>
      <Text textAlign="right" fontFamily="mono" whiteSpace="nowrap" data-testid="block-txs">
        <TxCount b={b} />
      </Text>
      <Text hideBelow="c3" textAlign="right" fontFamily="mono">
        {blockTimeText(b.block_time_ms)}
      </Text>
      <Text hideBelow="c3" textAlign="right" fontFamily="mono" color="fg.muted">
        {sizeText(b.size_bytes)}
      </Text>
    </ListRow>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <StateBox
      tone="error"
      icon={<LuTriangleAlert size={20} />}
      title="The blocks could not be loaded"
      actions={
        <Button size="sm" colorPalette="blue" onClick={onRetry}>
          Try again
        </Button>
      }
    >
      The explorer did not answer. Try again in a moment.
    </StateBox>
  );
}

/**
 * `/blocks`: 25 blocks a page. Without `top` the newest blocks, polled; any
 * move away pins the list to the newest height seen (`?top=H&page=p`), and a
 * pinned page never changes.
 */
export function BlocksPage({ top, page }: BlocksPlace) {
  const pinned = top != null;
  const blocks = useBlocks(pinned ? { before: blocksPageBefore(top, page), limit: BLOCKS_PER_PAGE } : { limit: BLOCKS_PER_PAGE });
  const oldest = useStatus().data?.back_fill.oldest_height ?? null;
  const latest = useStats().data?.latest_height ?? null;
  const items = blocks.data?.items ?? [];
  // The height the pages count down from: the pin, or the newest block of the live page.
  const anchor = top ?? items[0]?.height ?? null;
  const last = anchor != null ? blocksLastPage(anchor, oldest) : null;
  const fresh = pinned && latest != null && latest > top ? latest - top : 0;

  return (
    <Box>
      <PageTitle
        title="Blocks"
        intro="A new block every few seconds, each proposed by a validator and holding the transactions of that moment."
      >
        <Button asChild variant="outline" size="sm">
          <NextLink href="/blocks/future">
            <LuClock /> Future block
          </NextLink>
        </Button>
      </PageTitle>
      {!blocks.data ? (
        blocks.error ? (
          <LoadError onRetry={() => void blocks.refetch()} />
        ) : (
          <TableSkeleton rows={10} columns={6} />
        )
      ) : (
        <ListPanel>
          <ListHead templateColumns={COLUMNS} aria-hidden>
            <Text>Height</Text>
            <Text hideBelow="c2">Time</Text>
            <Text>Proposer</Text>
            <Text textAlign="right">Transactions</Text>
            <Text hideBelow="c3" textAlign="right">
              Block time
            </Text>
            <Text hideBelow="c3" textAlign="right">
              Size
            </Text>
          </ListHead>
          <Box role="list" aria-label="Blocks">
            {items.map((b) => (
              <BlockRow key={b.height} b={b} />
            ))}
          </Box>
          {items.length === 0 && (
            <Text px="4" py="6" color="fg.muted" fontSize="sm" borderTopWidth="1px" borderColor="border.panel">
              {pinned ? "No blocks on this page." : "No blocks indexed yet."}
            </Text>
          )}
          {anchor != null && (
            <Pager
              page={page}
              last={last}
              hasNext={items.length === BLOCKS_PER_PAGE}
              href={(p) => blocksHref({ top: anchor, page: p })}
              firstHref="/blocks"
            >
              {items.length > 0 && (
                <HStack as="span" gap="1" color="fg.muted" data-testid="blocks-range">
                  Blocks{" "}
                  <Text as="span" fontFamily="mono">
                    {formatCount(items[0].height)}
                  </Text>{" "}
                  –{" "}
                  <Text as="span" fontFamily="mono">
                    {formatCount(items[items.length - 1].height)}
                  </Text>
                </HStack>
              )}
              {fresh > 0 && (
                <PagerPill href="/blocks" testId="blocks-new">
                  <Text as="span" fontFamily="mono">
                    {formatCount(fresh)}
                  </Text>{" "}
                  new block{fresh === 1 ? "" : "s"} · back to latest
                </PagerPill>
              )}
            </Pager>
          )}
        </ListPanel>
      )}
    </Box>
  );
}
