"use client";

import { Box, Button, Heading, HStack, Skeleton, Stack, Text } from "@chakra-ui/react";
import { useMemo, useState } from "react";
import { LuChevronDown } from "react-icons/lu";

import { useBlockTransfers } from "@/api/hooks";
import type { BlockTransfer } from "@/api/types";
import { StepList } from "@/components/funds-flow/step-list";
import { HashLink } from "@/components/ui/links";
import { formatCount } from "@/lib/format";
import { groupMeta, groupTransfers, phraseOf, type FlowGroupView } from "@/render/block-flow";

const panel = {
  bg: "bg.panel",
  borderWidth: "1px",
  borderColor: "border.panel",
  rounded: "l3",
} as const;

const keyOf = (t: BlockTransfer) => `${t.tx_index}:${t.seq}`;

/** The line under a step: what it did and where, "in 3F9A…C21E" or "block N, by the chain". */
function StepWhere({ t, height }: { t: BlockTransfer; height: number }) {
  const phrase = phraseOf(t);
  return (
    <Text as="span">
      {phrase && `${phrase} · `}
      {t.tx_hash ? (
        <>
          in <HashLink hash={t.tx_hash} noCopy fontSize="xs" verticalAlign="baseline" />
        </>
      ) : (
        <Text as="span" data-testid="by-the-chain">
          block {formatCount(height)}, by the chain
        </Text>
      )}
    </Text>
  );
}

function Steps({ rows, height }: { rows: BlockTransfer[]; height: number }) {
  return (
    <StepList
      transfers={rows}
      collapsed={false}
      inOrder
      keyOf={keyOf}
      describe={(t) => <StepWhere t={t} height={height} />}
    />
  );
}

function Toggle({ open, onClick, children }: { open: boolean; onClick: () => void; children: string }) {
  return (
    <Button variant="ghost" size="sm" minH="44px" aria-expanded={open} onClick={onClick} flexShrink="0">
      {children}
      <Box as="span" transform={open ? "rotate(180deg)" : undefined} display="inline-flex">
        <LuChevronDown />
      </Box>
    </Button>
  );
}

function Group({ g, height }: { g: FlowGroupView; height: number }) {
  const [open, setOpen] = useState(false);
  return (
    <Stack gap="2" py="3" borderTopWidth="1px" borderColor="border.panel" _first={{ borderTopWidth: "0" }} data-testid="flow-group" data-group={g.group}>
      <HStack justify="space-between" align="start" gap="3">
        <Stack gap="0.5" minW="0">
          <Text fontSize="sm" data-testid="flow-group-summary">
            <Text as="span" fontWeight="semibold">
              {g.label}
            </Text>
            {g.summary && ` · ${g.summary}`}
          </Text>
          <Text fontSize="xs" color="fg.muted" data-testid="flow-group-meta">
            {groupMeta(g)}
          </Text>
        </Stack>
        <Toggle open={open} onClick={() => setOpen((o) => !o)}>
          {open ? "Hide" : "Show"}
        </Toggle>
      </HStack>
      {open && <Steps rows={g.rows} height={height} />}
    </Stack>
  );
}

/** The group lines while the transfers load. */
export function FlowSkeleton({ lines = 3 }: { lines?: number }) {
  return (
    <Stack gap="0" {...panel} px={{ base: "4", md: "5" }} aria-hidden data-testid="flow-skeleton">
      {Array.from({ length: lines }).map((_, i) => (
        <Stack key={i} gap="2" py="3" borderTopWidth={i === 0 ? "0" : "1px"} borderColor="border.panel">
          <Skeleton h="4" w="70%" />
          <Skeleton h="3" w="30%" />
        </Stack>
      ))}
    </Stack>
  );
}

/**
 * "Where the funds went in this block": every transfer at the height grouped
 * by what happened, each group expanding to its steps, and every step at once.
 */
export function BlockFlow({ height, count }: { height: number; count: number }) {
  const { data, error, refetch } = useBlockTransfers(height);
  const [all, setAll] = useState(false);
  const groups = useMemo(() => (data ? groupTransfers(data) : []), [data]);
  const total = data?.length ?? count;

  return (
    <Stack as="section" gap="3" aria-labelledby="block-flow-title" data-testid="block-flow">
      <Stack gap="1">
        <Heading as="h2" size="lg" id="block-flow-title">
          Where the funds went in this block
        </Heading>
        <Text fontSize="sm" color="fg.muted">
          Every coin that moved, grouped by what happened. Steps the chain does by itself are marked &ldquo;by the chain&rdquo;.
        </Text>
      </Stack>
      {!data ? (
        error ? (
          <HStack justify="space-between" wrap="wrap" gap="3" {...panel} p="4">
            <Text fontSize="sm" color="fg.muted">
              The block&apos;s transfers could not be loaded.
            </Text>
            <Button colorPalette="blue" minH="44px" onClick={() => void refetch()}>
              Try again
            </Button>
          </HStack>
        ) : (
          <FlowSkeleton />
        )
      ) : (
        <>
          <Box {...panel} px={{ base: "4", md: "5" }}>
            {groups.map((g) => (
              <Group key={g.group} g={g} height={height} />
            ))}
          </Box>
          <Box>
            <Toggle open={all} onClick={() => setAll((o) => !o)}>
              {all ? "Hide the transfers" : `Show all ${formatCount(total)} transfers, one by one`}
            </Toggle>
          </Box>
          {all && (
            <Box {...panel} px={{ base: "4", md: "5" }} py="3" data-testid="block-flow-all">
              <Steps rows={data} height={height} />
            </Box>
          )}
        </>
      )}
    </Stack>
  );
}
