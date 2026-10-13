"use client";

import { Box, Button, HStack, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useRef, useState } from "react";
import { LuTriangleAlert } from "react-icons/lu";

import { useTransactions } from "@/api/hooks";
import type { TxListParams, TxStatusFilter, TxSummary } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { FilterPills } from "@/components/ui/filter-pills";
import { HashLink } from "@/components/ui/links";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { PageTitle } from "@/components/ui/page-head";
import { PagerPill, PrevNextPager } from "@/components/ui/pager";
import { TableSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { StatusPill } from "@/components/ui/status-pill";
import { TimeAgo } from "@/components/ui/time-ago";
import { formatCount } from "@/lib/format";
import { coinsText } from "@/render/messages/common";
import type { RenderContext } from "@/render/messages/types";
import { txOneLine } from "@/render/txs";

import { useListRenderContext } from "./list-context";

export const TXS_PER_PAGE = 25;

// Hash (c2) · What happened · By (c2) · Status · Fee (c3) · Time.
// (Custom breakpoints are not in Chakra's generated types, so these objects are hoisted.)
// Status is a fixed 90px from c2: an `auto` column sizes to each row's own tag,
// so a "Failed" row would shift the columns after it.
const COLUMNS = {
  base: "minmax(0, 1fr) auto auto",
  c2: "110px minmax(0, 1fr) minmax(0, 170px) 90px 116px",
  c3: "110px minmax(0, 1fr) minmax(0, 170px) 90px 110px 116px",
};

/** The status pills and the `status` each asks the list for. */
export const STATUS_FILTERS: { label: string; value: TxStatusFilter | null }[] = [
  { label: "All", value: null },
  { label: "Success", value: "success" },
  { label: "Failed", value: "failed" },
];

/** Where a transaction stands in the chain: the list's order, newest first. */
type TxKey = Pick<TxSummary, "height" | "tx_index">;

const newer = (a: TxKey, b: TxKey) => a.height > b.height || (a.height === b.height && a.tx_index > b.tx_index);

/**
 * How many rows of the live first page are newer than the pinned top
 * transaction: "25+" when the whole (full) page is.
 */
export function newTxCount(live: TxKey[], top: TxKey): string | null {
  const n = live.filter((t) => newer(t, top)).length;
  if (n === 0) return null;
  return n === live.length && n >= TXS_PER_PAGE ? `${TXS_PER_PAGE}+` : formatCount(n);
}

function TxRow({ tx, ctx }: { tx: TxSummary; ctx: RenderContext }) {
  return (
    <ListRow role="listitem" templateColumns={COLUMNS} data-testid="tx-row">
      <Box hideBelow="c2" minW="0">
        <HashLink hash={tx.hash} noCopy />
      </Box>
      <Link asChild color="fg" fontSize="13px" minW="0" _hover={{ color: "accent.fg" }}>
        <NextLink href={`/txs/${tx.hash}`} data-testid="tx-what">
          {txOneLine(tx, ctx)}
        </NextLink>
      </Link>
      <Box hideBelow="c2" minW="0">
        {tx.signer ? (
          <AccountChip address={tx.signer} label={ctx.label(tx.signer)} />
        ) : (
          <Text as="span" color="fg.muted">
            —
          </Text>
        )}
      </Box>
      <Box>
        <StatusPill success={tx.success} />
      </Box>
      <Text hideBelow="c3" textAlign="right" fontFamily="mono" fontSize="13px" color="fg.muted" truncate>
        {tx.fee.length ? coinsText(tx.fee, ctx) : "none"}
      </Text>
      <Box textAlign="right" whiteSpace="nowrap">
        <TimeAgo time={tx.time} fontSize="13px" />
        <Text fontSize="11px" color="fg.muted" mt="2px" data-testid="tx-height">
          block{" "}
          <Link asChild fontFamily="mono" color="accent.fg">
            <NextLink href={`/blocks/${tx.height}`}>{formatCount(tx.height)}</NextLink>
          </Link>
        </Text>
      </Box>
    </ListRow>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <StateBox
      tone="error"
      icon={<LuTriangleAlert size={20} />}
      title="The transactions could not be loaded"
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

/** Where the list is: the cursors of the pages after the first, and the top transaction they hang from. */
interface Place {
  cursors: string[];
  top: TxKey | null;
}

const LIVE: Place = { cursors: [], top: null };

/**
 * `/txs`: 25 transactions a page, newest first. Page 1 is live (polled);
 * "next" pins the list to its top transaction and pages by the API cursor,
 * kept as a stack so "previous" returns exactly. The pin lives in the
 * component, never in the URL: a reload is back on the live page.
 */
export function TxsPage() {
  const [status, setStatus] = useState<TxStatusFilter | null>(null);
  const [place, setPlace] = useState<Place>(LIVE);
  const listTop = useRef<HTMLDivElement>(null);
  const ctx = useListRenderContext();

  const base: TxListParams = { limit: TXS_PER_PAGE, status: status ?? undefined };
  const cursor = place.cursors.at(-1);
  // The live first page always polls: it is page 1, or the background check behind the pill.
  const live = useTransactions(base);
  const shown = useTransactions(cursor ? { ...base, cursor } : base);
  const page = place.cursors.length + 1;
  const items = shown.data?.items ?? [];
  const next = shown.data?.next_cursor ?? null;

  const go = (to: Place) => {
    setPlace(to);
    // Back on the live page: the latest now, not the last poll's.
    if (to === LIVE) void live.refetch();
    listTop.current?.scrollIntoView?.({ block: "start" });
  };
  const fresh = place.top && live.data ? newTxCount(live.data.items, place.top) : null;

  return (
    <Box>
      <PageTitle title="Transactions" intro="Everything people did on the chain, newest first, each in one line." />
      <Box ref={listTop} scrollMarginTop="4">
        <FilterPills
          label="Filter by status"
          options={STATUS_FILTERS}
          value={status}
          onChange={(v) => {
            setStatus(v);
            setPlace(LIVE);
          }}
        />
      </Box>
      {!shown.data ? (
        shown.error ? (
          <LoadError onRetry={() => void shown.refetch()} />
        ) : (
          // A pill change or a new page is a different list: its rows load as skeletons.
          <TableSkeleton rows={10} columns={6} />
        )
      ) : (
        <ListPanel>
          <ListHead templateColumns={COLUMNS} aria-hidden>
            <Text hideBelow="c2">Hash</Text>
            <Text>What happened</Text>
            <Text hideBelow="c2">By</Text>
            <Text>Status</Text>
            <Text hideBelow="c3" textAlign="right">
              Fee
            </Text>
            <Text textAlign="right">Time</Text>
          </ListHead>
          <Box role="list" aria-label="Transactions">
            {items.map((tx) => (
              <TxRow key={tx.hash} tx={tx} ctx={ctx} />
            ))}
          </Box>
          {items.length === 0 && (
            <Text px="4" py="6" color="fg.muted" fontSize="sm">
              {page > 1 ? "No older transactions." : status === "failed" ? "No failed transactions." : "No transactions indexed yet."}
            </Text>
          )}
          <PrevNextPager
            page={page}
            onFirst={page > 1 ? () => go(LIVE) : null}
            onPrev={page > 1 ? () => go(page === 2 ? LIVE : { ...place, cursors: place.cursors.slice(0, -1) }) : null}
            onNext={
              next
                ? () => go({ cursors: [...place.cursors, next], top: place.top ?? (items[0] ? { height: items[0].height, tx_index: items[0].tx_index } : null) })
                : null
            }
          >
            <HStack as="span" gap="1" color="fg.muted">
              Newest first ·{" "}
              <Text as="span" fontFamily="mono">
                {TXS_PER_PAGE}
              </Text>{" "}
              per page
            </HStack>
            {fresh && (
              <PagerPill onClick={() => go(LIVE)} testId="txs-new">
                <Text as="span" fontFamily="mono">
                  {fresh}
                </Text>{" "}
                new transaction{fresh === "1" ? "" : "s"} · back to latest
              </PagerPill>
            )}
          </PrevNextPager>
        </ListPanel>
      )}
    </Box>
  );
}
