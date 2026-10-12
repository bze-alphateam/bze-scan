"use client";

import { Badge, Box, Button, Circle, HStack, Link, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useMemo, useState } from "react";

import { useAccountActivity, useAccountIbc, useDenoms } from "@/api/hooks";
import type { Activity, ActivityCategory, IbcTransfer } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { categoryLook } from "@/components/ui/action-card";
import { Card } from "@/components/ui/card";
import { HashLink } from "@/components/ui/links";
import { ListSkeleton } from "@/components/ui/skeleton";
import { TimeAgo } from "@/components/ui/time-ago";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount } from "@/lib/format";
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
const NO_IBC: IbcTransfer[] = [];

/** Keeps the category in the URL (`?tab=activity&category=dex`) without a navigation. */
function writeCategory(category: ActivityCategory | null) {
  const url = new URL(window.location.href);
  if (category) {
    url.searchParams.set("tab", "activity");
    url.searchParams.set("category", category);
  } else {
    url.searchParams.delete("tab");
    url.searchParams.delete("category");
  }
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
  const look = categoryLook(actionCategory(row.category));
  const Icon = look.icon;
  const lines = deltaLines(row, ctx);
  return (
    <HStack
      role="listitem"
      align="start"
      gap="3"
      px="4"
      py="3"
      borderTopWidth="1px"
      borderColor="border.panel"
      data-testid="activity-row"
      data-kind={row.kind}
    >
      <Circle size="9" bg={`${look.palette}.subtle`} color={`${look.palette}.fg`} flexShrink={0} aria-hidden>
        <Icon />
      </Circle>
      <Stack gap="1" minW="0" flex="1">
        <Box fontSize="sm" lineHeight="tall" wordBreak="break-word" data-testid="activity-what">
          {line.parts.map((p, i) => (
            <PartView key={i} part={p} />
          ))}
          {line.chips.map((c) => (
            <Badge key={c.label} colorPalette={c.palette} variant="subtle" rounded="full" size="sm" ms="2" data-testid="activity-chip">
              {c.label}
            </Badge>
          ))}
        </Box>
        <Text fontSize="xs" color="fg.muted" data-testid="activity-when">
          <TimeAgo time={row.time} />
          {" · "}
          {row.tx_hash ? (
            <HashLink hash={row.tx_hash} noCopy />
          ) : (
            <>
              settled in block{" "}
              <Link asChild>
                <NextLink href={`/blocks/${row.height}`}>{formatCount(row.height)}</NextLink>
              </Link>
              , {BLOCK_LEVEL_NOTE}
            </>
          )}
        </Text>
        {!row.success && (
          <Text fontSize="xs" color="red.fg" data-testid="activity-failed">
            Failed · see the transaction for the reason
          </Text>
        )}
      </Stack>
      <Stack gap="0.5" align="end" flexShrink={0} maxW="45%" data-testid="activity-amounts">
        {lines.length === 0 ? (
          <Text fontSize="sm" color="fg.muted">
            —
          </Text>
        ) : (
          lines.map((l) => (
            <Text
              key={l.text}
              fontSize={l.fee ? "xs" : "sm"}
              color={l.positive ? "green.fg" : l.fee ? "fg.muted" : undefined}
              fontVariantNumeric="tabular-nums"
              textAlign="right"
              wordBreak="break-word"
            >
              {l.text}
            </Text>
          ))
        )}
      </Stack>
    </HStack>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <Card title="The activity could not be loaded">
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

/** The account's Activity tab: category chips, one row per transaction (or block), polled until older rows are loaded. */
export function ActivityTab({ address, initialCategory = null }: { address: string; initialCategory?: ActivityCategory | null }) {
  const [category, setCategory] = useState<ActivityCategory | null>(initialCategory);
  const list = useAccountActivity(address, { category });
  const ibc = useAccountIbc(address);
  const denoms = useDenoms();
  const ctx = useMemo(() => createRenderContext({ transfers: NO_TRANSFERS, tokens: denoms.data }), [denoms.data]);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const transfers = ibc.data ?? NO_IBC;
  return (
    <Stack gap="4">
      <Box overflowX="auto" mx={{ base: "-4", md: "0" }} px={{ base: "4", md: "0" }}>
        <HStack gap="2" wrap={{ base: "nowrap", md: "wrap" }} role="group" aria-label="Show activity">
          {ACTIVITY_FILTERS.map((f) => (
            <Button
              key={f.label}
              size="sm"
              minH="44px"
              rounded="full"
              flexShrink={0}
              variant={category === f.value ? "subtle" : "outline"}
              aria-pressed={category === f.value}
              onClick={() => {
                setCategory(f.value);
                writeCategory(f.value);
              }}
            >
              {f.label}
            </Button>
          ))}
        </HStack>
      </Box>
      {list.snapshot && (
        <HStack justify="space-between" wrap="wrap" gap="3" data-testid="activity-snapshot">
          <Text fontSize="sm" color="fg.muted">
            Showing older activity: the list stopped updating.
          </Text>
          <Button variant="outline" size="sm" minH="44px" onClick={() => list.latest()}>
            Back to the latest activity
          </Button>
        </HStack>
      )}
      {!list.data ? (
        list.error ? (
          <LoadError onRetry={() => void list.refetch()} />
        ) : (
          // The first open and every chip change: a different list, loading.
          <ListSkeleton rows={8} />
        )
      ) : items.length === 0 ? (
        <Card title="No activity in this category yet">
          <Text fontSize="sm" color="fg.muted">
            What happens to this account will show up here, newest first.
          </Text>
        </Card>
      ) : (
        <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" overflow="hidden">
          <Box role="list" aria-label="Activity" mt="-1px">
            {items.map((row) => (
              <ActivityRow
                key={`${row.height}:${row.tx_hash ?? "block"}`}
                row={row}
                address={address}
                ctx={ctx}
                transfers={transfers}
              />
            ))}
          </Box>
        </Box>
      )}
      {list.isFetchingNextPage && <ListSkeleton rows={3} />}
      {list.hasNextPage && !list.isFetchingNextPage && (
        <Button variant="outline" size="sm" minH="44px" alignSelf="start" onClick={() => void list.fetchNextPage()}>
          Show older activity
        </Button>
      )}
    </Stack>
  );
}
