"use client";

import { Box, Button, Grid, Heading, HStack, Stack, Text } from "@chakra-ui/react";
import { useState } from "react";

import { useTransactionList } from "@/api/hooks";
import type { TxStatusFilter, TxSummary } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Card } from "@/components/ui/card";
import { HashLink } from "@/components/ui/links";
import { TableSkeleton } from "@/components/ui/skeleton";
import { StatusPill } from "@/components/ui/status-pill";
import { TimeAgo } from "@/components/ui/time-ago";
import { formatCount } from "@/lib/format";
import { coinsText } from "@/render/messages/common";
import type { RenderContext } from "@/render/messages/types";
import { txOneLine } from "@/render/txs";

import { useListRenderContext } from "./list-context";

const COLUMNS = {
  base: "minmax(0, 1fr) auto",
  md: "110px minmax(0, 2fr) minmax(0, 1fr) 104px minmax(0, 0.8fr) 120px",
};

/** The status chips and the `status` each asks the list for. */
export const STATUS_FILTERS: { label: string; value: TxStatusFilter | null }[] = [
  { label: "All", value: null },
  { label: "Success", value: "success" },
  { label: "Failed", value: "failed" },
];

function PhoneLabel({ children }: { children: string }) {
  return (
    <Text as="span" hideFrom="md" fontSize="xs" color="fg.muted" me="1">
      {children}
    </Text>
  );
}

function TxRow({ tx, ctx }: { tx: TxSummary; ctx: RenderContext }) {
  return (
    <Grid
      role="listitem"
      templateColumns={COLUMNS}
      gap={{ base: "1", md: "4" }}
      alignItems="center"
      px="4"
      py="3"
      minH="56px"
      borderTopWidth="1px"
      borderColor="border.panel"
      data-testid="tx-row"
    >
      <HashLink hash={tx.hash} noCopy />
      <Box gridRow={{ base: "1", md: "auto" }} gridColumn={{ base: "2", md: "4" }} justifySelf={{ base: "end", md: "start" }}>
        <StatusPill success={tx.success} size="sm" />
      </Box>
      <Text fontSize="sm" gridColumn={{ base: "1 / -1", md: "2" }} gridRow={{ md: "1" }} data-testid="tx-what">
        {txOneLine(tx, ctx)}
      </Text>
      <Box minW="0" gridColumn={{ base: "1 / -1", md: "3" }} gridRow={{ md: "1" }}>
        <PhoneLabel>By</PhoneLabel>
        {tx.signer ? (
          <AccountChip address={tx.signer} />
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
      <Stack gap="0" gridColumn={{ base: "1 / -1", md: "6" }} gridRow={{ md: "1" }}>
        <TimeAgo time={tx.time} fontSize="sm" />
        <Text fontSize="xs" color="fg.muted" data-testid="tx-height">
          block {formatCount(tx.height)}
        </Text>
      </Stack>
    </Grid>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <Card title="The transactions could not be loaded">
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

/** `/txs`: the latest transactions of a status, polled, until older ones are loaded. */
export function TxsPage() {
  const [status, setStatus] = useState<TxStatusFilter | null>(null);
  const list = useTransactionList(status);
  const ctx = useListRenderContext();
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Stack gap="2">
        <Heading as="h1" size={{ base: "2xl", md: "3xl" }}>
          Transactions
        </Heading>
        <Text color="fg.muted" maxW="3xl">
          Everything people did on the chain, newest first, each in one line.
        </Text>
      </Stack>
      <HStack gap="2" wrap="wrap" role="group" aria-label="Show transactions">
        {STATUS_FILTERS.map((f) => (
          <Button
            key={f.label}
            size="sm"
            minH="44px"
            rounded="full"
            variant={status === f.value ? "subtle" : "outline"}
            aria-pressed={status === f.value}
            onClick={() => setStatus(f.value)}
          >
            {f.label}
          </Button>
        ))}
      </HStack>
      {list.snapshot && (
        <HStack justify="space-between" wrap="wrap" gap="3" data-testid="txs-snapshot">
          <Text fontSize="sm" color="fg.muted">
            Showing older transactions: the list stopped updating.
          </Text>
          <Button
            variant="outline"
            size="sm"
            minH="44px"
            onClick={() => {
              list.latest();
              window.scrollTo({ top: 0 });
            }}
          >
            Back to the latest transactions
          </Button>
        </HStack>
      )}
      {!list.data ? (
        list.error ? (
          <LoadError onRetry={() => void list.refetch()} />
        ) : (
          // A chip change is a different list: its rows load as skeletons.
          <TableSkeleton rows={10} columns={6} />
        )
      ) : (
        <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" overflow="hidden">
          <Grid
            hideBelow="md"
            templateColumns={COLUMNS}
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
            <Text>Time</Text>
          </Grid>
          <Box role="list" aria-label="Transactions">
            {items.map((tx) => (
              <TxRow key={tx.hash} tx={tx} ctx={ctx} />
            ))}
          </Box>
          {items.length === 0 && (
            <Text px="4" py="6" color="fg.muted" fontSize="sm" borderTopWidth="1px" borderColor="border.panel">
              {status === "failed" ? "No failed transactions." : "No transactions indexed yet."}
            </Text>
          )}
        </Box>
      )}
      {list.isFetchingNextPage && <TableSkeleton rows={5} columns={6} />}
      {list.hasNextPage && !list.isFetchingNextPage && (
        <Button variant="outline" size="sm" minH="44px" alignSelf="start" onClick={() => void list.fetchNextPage()}>
          Show older transactions
        </Button>
      )}
    </Stack>
  );
}
