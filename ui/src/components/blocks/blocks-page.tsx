"use client";

import { Box, Button, Grid, Heading, HStack, Link, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";

import { useBlocks } from "@/api/hooks";
import type { BlockSummary } from "@/api/types";
import { Card } from "@/components/ui/card";
import { TableSkeleton } from "@/components/ui/skeleton";
import { TimeAgo } from "@/components/ui/time-ago";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount } from "@/lib/format";
import { blockTimeText, sizeText } from "@/render/blocks";

const COLUMNS = { base: "minmax(0, 1fr) auto", md: "120px 120px minmax(0, 1.6fr) 120px 96px 80px" };

/** A label shown before a value on phones, where the header row is hidden. */
function PhoneLabel({ children }: { children: string }) {
  return (
    <Text as="span" hideFrom="md" fontSize="xs" color="fg.muted" me="1">
      {children}
    </Text>
  );
}

/** "7" and "· 1 failed" in red when some failed. */
export function TxCount({ b }: { b: Pick<BlockSummary, "tx_count" | "tx_failed_count"> }) {
  return (
    <>
      {formatCount(b.tx_count)}
      {b.tx_failed_count > 0 && (
        <Text as="span" color="red.fg">
          {" "}
          · {formatCount(b.tx_failed_count)} failed
        </Text>
      )}
    </>
  );
}

function BlockRow({ b }: { b: BlockSummary }) {
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
      data-testid="block-row"
    >
      <Link asChild fontWeight="medium" fontVariantNumeric="tabular-nums">
        <NextLink href={`/blocks/${b.height}`}>{formatCount(b.height)}</NextLink>
      </Link>
      <TimeAgo time={b.time} fontSize="sm" color="fg.muted" justifySelf={{ base: "end", md: "start" }} />
      <Box minW="0" gridColumn={{ base: "1 / -1", md: "auto" }}>
        <PhoneLabel>Proposer</PhoneLabel>
        {b.proposer ? (
          <ValidatorChip operator={b.proposer.operator_address} moniker={b.proposer.moniker} />
        ) : (
          <Text as="span" color="fg.muted">
            —
          </Text>
        )}
      </Box>
      <Text fontSize="sm" gridColumn={{ base: "1 / -1", md: "auto" }} data-testid="block-txs">
        <PhoneLabel>Transactions</PhoneLabel>
        <TxCount b={b} />
      </Text>
      <Text fontSize="sm" gridColumn={{ base: "1 / -1", md: "auto" }}>
        <PhoneLabel>Block time</PhoneLabel>
        {blockTimeText(b.block_time_ms)}
      </Text>
      <Text fontSize="sm" color="fg.muted" gridColumn={{ base: "1 / -1", md: "auto" }}>
        <PhoneLabel>Size</PhoneLabel>
        {sizeText(b.size_bytes)}
      </Text>
    </Grid>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <Card title="The blocks could not be loaded">
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

/** `/blocks`: the latest blocks, polled, until older ones are loaded. */
export function BlocksPage() {
  const list = useBlocks();
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Stack gap="2">
        <Heading as="h1" size={{ base: "2xl", md: "3xl" }}>
          Blocks
        </Heading>
        <Text color="fg.muted" maxW="3xl">
          A new block every few seconds, each proposed by a validator and holding the transactions of that moment.
        </Text>
      </Stack>
      {list.snapshot && (
        <HStack justify="space-between" wrap="wrap" gap="3" data-testid="blocks-snapshot">
          <Text fontSize="sm" color="fg.muted">
            Showing older blocks: the list stopped updating.
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
            Back to the latest blocks
          </Button>
        </HStack>
      )}
      {!list.data ? (
        list.error ? (
          <LoadError onRetry={() => void list.refetch()} />
        ) : (
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
            <Text>Height</Text>
            <Text>Time</Text>
            <Text>Proposer</Text>
            <Text>Transactions</Text>
            <Text>Block time</Text>
            <Text>Size</Text>
          </Grid>
          <Box role="list" aria-label="Blocks">
            {items.map((b) => (
              <BlockRow key={b.height} b={b} />
            ))}
          </Box>
          {items.length === 0 && (
            <Text px="4" py="6" color="fg.muted" fontSize="sm" borderTopWidth="1px" borderColor="border.panel">
              No blocks indexed yet.
            </Text>
          )}
        </Box>
      )}
      {list.isFetchingNextPage && <TableSkeleton rows={5} columns={6} />}
      {list.hasNextPage && !list.isFetchingNextPage && (
        <Button variant="outline" size="sm" minH="44px" alignSelf="start" onClick={() => void list.fetchNextPage()}>
          Show older blocks
        </Button>
      )}
    </Stack>
  );
}
