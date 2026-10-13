"use client";

import { Box, Button, Grid, Link, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useMemo, useState, type ReactNode } from "react";
import { LuTriangleAlert } from "react-icons/lu";

import { ACTIVITY_PER_PAGE, useAccountActivity, useAccountIbc, useDenoms, useStatus } from "@/api/hooks";
import type { Activity, ActivityCategory, IbcTransfer } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { categoryLook } from "@/components/ui/action-card";
import { Chip } from "@/components/ui/chip";
import { FilterPills } from "@/components/ui/filter-pills";
import { HashLink } from "@/components/ui/links";
import { ListPanel } from "@/components/ui/list";
import { Pager, PagerPill } from "@/components/ui/pager";
import { ListSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { TimeAgo } from "@/components/ui/time-ago";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount } from "@/lib/format";
import { activityRange, activitySearch, lastPage } from "@/render/account";
import {
  ACTIVITY_FILTERS,
  actionCategory,
  activityLine,
  BLOCK_LEVEL_NOTE,
  deltaLines,
  ibcFor,
  type Part,
} from "@/render/activity";
import { createRenderContext } from "@/render/messages/registry";
import type { RenderContext } from "@/render/messages/types";

const NO_TRANSFERS: never[] = [];
const CHIP_LOOK = { orange: "warn", green: "ok", red: "bad" } as const;
const NO_IBC: IbcTransfer[] = [];

// Icon · what happened · the account's own net; on phones the net goes under the words.
const ROW_COLUMNS = { base: "32px minmax(0, 1fr)", c2: "32px minmax(0, 1fr) auto" };
const NET_COLUMN = { base: "2", c2: "auto" };
const NET_ALIGN = { base: "flex-start", c2: "flex-end" };

/** Keeps the place in the URL (`?tab=activity&category=dex&page=3`) without a navigation. */
function writePlace(category: ActivityCategory | null, page: number) {
  const url = new URL(window.location.href);
  url.search = activitySearch(category, page);
  window.history.replaceState(window.history.state, "", url);
}

function PartView({ part }: { part: Part }) {
  if (typeof part === "string") return <>{part}</>;
  const p = part.party;
  return p.address.startsWith("bzevaloper1") ? (
    <ValidatorChip operator={p.address} moniker={p.moniker} verticalAlign="middle" />
  ) : (
    <AccountChip address={p.address} label={p.label} verticalAlign="middle" />
  );
}

function ActivityRow({ row, address, ctx, transfers }: { row: Activity; address: string; ctx: RenderContext; transfers: IbcTransfer[] }) {
  const line = activityLine(row, { address, ctx, ibc: ibcFor(row, transfers) });
  const Icon = categoryLook(actionCategory(row.category)).icon;
  const lines = deltaLines(row, ctx);
  return (
    <Grid
      role="listitem"
      templateColumns={ROW_COLUMNS}
      columnGap="3"
      rowGap="1"
      alignItems="center"
      px="4"
      py="3"
      borderBottomWidth="1px"
      borderColor="border.panel"
      data-testid="activity-row"
      data-kind={row.kind}
    >
      <Box display="grid" placeItems="center" boxSize="32px" rounded="l2" bg="accent.subtle" color="accent.fg" aria-hidden>
        <Icon size={16} />
      </Box>
      <Stack gap="2px" minW="0">
        <Box fontSize="14px" lineHeight="1.5" wordBreak="break-word" data-testid="activity-what">
          {line.parts.map((p, i) => (
            <PartView key={i} part={p} />
          ))}
          {line.chips.map((c) => (
            <Chip key={c.label} variant={CHIP_LOOK[c.palette]} ms="2" data-testid="activity-chip">
              {c.label}
            </Chip>
          ))}
        </Box>
        <Text fontSize="12px" color="fg.muted" data-testid="activity-when">
          <TimeAgo time={row.time} />
          {" · "}
          {row.tx_hash ? (
            <HashLink hash={row.tx_hash} noCopy fontSize="12px" />
          ) : (
            <>
              settled in block{" "}
              <Link asChild fontFamily="mono">
                <NextLink href={`/blocks/${row.height}`}>{formatCount(row.height)}</NextLink>
              </Link>
              , {BLOCK_LEVEL_NOTE}
            </>
          )}
        </Text>
        {!row.success && (
          <Text fontSize="12px" color="neg.fg" data-testid="activity-failed">
            Failed · see the transaction for the reason
          </Text>
        )}
      </Stack>
      <Stack
        gap="1px"
        gridColumn={NET_COLUMN}
        align={NET_ALIGN}
        fontFamily="mono"
        fontSize="13px"
        whiteSpace="nowrap"
        data-testid="activity-amounts"
      >
        {lines.length === 0 ? (
          <Text color="fg.muted">—</Text>
        ) : (
          lines.map((l) => (
            <Text key={l.text} fontSize={l.fee ? "11px" : undefined} color={l.positive ? "pos.fg" : l.fee ? "fg.muted" : undefined}>
              {l.text}
            </Text>
          ))
        )}
      </Stack>
    </Grid>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <StateBox
      tone="error"
      icon={<LuTriangleAlert size={20} />}
      title="The activity could not be loaded"
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

/** A panel saying there is nothing to list: a bold line and a muted paragraph. */
export function EmptyPanel({ title, children, testId }: { title: string; children: ReactNode; testId?: string }) {
  return (
    <ListPanel px="5" py="18px" data-testid={testId}>
      <Text fontWeight="600" mb="1">
        {title}
      </Text>
      <Text color="fg.muted" fontSize="13px" lineHeight="1.55" maxW="720px">
        {children}
      </Text>
    </ListPanel>
  );
}

/** An address the explorer never saw do anything: its history is empty, its balances still right. */
function NeverSeen() {
  const oldest = useStatus().data?.back_fill.oldest_height ?? null;
  return (
    <EmptyPanel title="No activity seen yet" testId="activity-never">
      Nothing this account did is in the explorer&apos;s history
      {oldest != null && (
        <>
          {" "}
          (from block{" "}
          <Text as="span" fontFamily="mono">
            {formatCount(oldest)}
          </Text>
          )
        </>
      )}
      . Balances above are read live from the chain, so they are right even when the history is empty.
    </EmptyPanel>
  );
}

interface ActivityTabProps {
  address: string;
  initialCategory?: ActivityCategory | null;
  initialPage?: number;
  /** The explorer never saw the address do anything (no activity at all). */
  neverSeen?: boolean;
}

/**
 * The account's Activity tab: category pills, then 25 rows a numbered page,
 * newest first. Page 1 is polled; an older page stays as it was read, and a
 * pill says how many rows came in since, linking back to the newest.
 */
export function ActivityTab({ address, initialCategory = null, initialPage = 1, neverSeen = false }: ActivityTabProps) {
  const [category, setCategory] = useState<ActivityCategory | null>(initialCategory);
  const [page, setPage] = useState(initialPage);
  // The total this place was read with; the polled page 1 shows how many came after.
  const list = useAccountActivity(address, { category, page, enabled: !neverSeen });
  const latest = useAccountActivity(address, { category, page: 1, limit: 1, enabled: !neverSeen && page > 1 });
  const ibc = useAccountIbc(address);
  const denoms = useDenoms();
  const ctx = useMemo(() => createRenderContext({ transfers: NO_TRANSFERS, tokens: denoms.data }), [denoms.data]);

  if (neverSeen) return <NeverSeen />;

  const go = (next: ActivityCategory | null, n: number) => {
    setCategory(next);
    setPage(n);
    writePlace(next, n);
  };
  const items = list.data?.items ?? [];
  const transfers = ibc.data ?? NO_IBC;
  const total = list.data?.total ?? null;
  const last = total != null ? lastPage(total, ACTIVITY_PER_PAGE) : null;
  const fresh = page > 1 && latest.data && total != null ? latest.data.total - total : 0;
  const href = (n: number) => `/accounts/${address}${activitySearch(category, n)}`;

  return (
    <Box>
      <Box overflowX="auto" mx={{ base: "-4", md: "0" }} px={{ base: "4", md: "0" }}>
        <FilterPills
          label="Show activity"
          options={ACTIVITY_FILTERS}
          value={category}
          onChange={(c) => go(c, 1)}
          mb="14px"
        />
      </Box>
      {!list.data ? (
        list.error ? (
          <LoadError onRetry={() => void list.refetch()} />
        ) : (
          // The first open and every move: a different page, loading.
          <ListSkeleton rows={8} />
        )
      ) : items.length === 0 && page === 1 ? (
        <EmptyPanel title="No activity in this category yet">What happens to this account will show up here, newest first.</EmptyPanel>
      ) : (
        <ListPanel>
          <Box role="list" aria-label="Activity">
            {items.map((row) => (
              <ActivityRow key={`${row.height}:${row.tx_hash ?? "block"}`} row={row} address={address} ctx={ctx} transfers={transfers} />
            ))}
          </Box>
          {items.length === 0 && (
            <Text px="4" py="6" color="fg.muted" fontSize="sm" borderBottomWidth="1px" borderColor="border.panel">
              No activity on this page.
            </Text>
          )}
          <Box mt="-1px">
            <Pager
              page={page}
              last={last}
              hasNext={page < (last ?? 1)}
              href={href}
              firstHref={href(1)}
              numbered
              onGo={(n) => go(category, n)}
            >
              <Text as="span" fontFamily="mono" color="fg.muted" data-testid="activity-range">
                {activityRange(page, ACTIVITY_PER_PAGE, total ?? 0)}
              </Text>
              {fresh > 0 && (
                <PagerPill onClick={() => go(category, 1)} testId="activity-new">
                  <Text as="span" fontFamily="mono">
                    {formatCount(fresh)}
                  </Text>{" "}
                  new · back to latest
                </PagerPill>
              )}
            </Pager>
          </Box>
        </ListPanel>
      )}
    </Box>
  );
}
