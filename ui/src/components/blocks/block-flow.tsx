"use client";

import { Box, Button, Flex, HStack, Skeleton, Stack, Text } from "@chakra-ui/react";
import { useMemo, useState } from "react";
import { LuChevronDown } from "react-icons/lu";

import { useBlockTransfers } from "@/api/hooks";
import type { BlockTransfer } from "@/api/types";
import { StepList } from "@/components/funds-flow/step-list";
import { HashLink } from "@/components/ui/links";
import { ListPanel } from "@/components/ui/list";
import { SectionHead } from "@/components/ui/page-head";
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

const Chevron = ({ open }: { open: boolean }) => (
  <Box as="span" display="inline-flex" transition="transform 0.15s" transform={open ? "rotate(180deg)" : undefined}>
    <LuChevronDown size={12} />
  </Box>
);

/** "Show all N transfers, one by one": an accent text toggle. */
function Toggle({ open, onClick, children }: { open: boolean; onClick: () => void; children: string }) {
  return (
    <Button
      variant="plain"
      size="sm"
      minH="44px"
      px="0"
      gap="1.5"
      color="accent.fg"
      fontSize="13px"
      fontWeight="500"
      aria-expanded={open}
      onClick={onClick}
    >
      {children}
      <Chevron open={open} />
    </Button>
  );
}

/** A group: its summary row opens to its steps. */
function Group({ g, height }: { g: FlowGroupView; height: number }) {
  const [open, setOpen] = useState(false);
  return (
    <Box borderBottomWidth="1px" borderColor="border.panel" _last={{ borderBottomWidth: "0" }} data-testid="flow-group" data-group={g.group}>
      <Flex
        as="button"
        w="full"
        justify="space-between"
        align="center"
        gap="4"
        px="4"
        py="3"
        textAlign="start"
        cursor="pointer"
        bg={open ? "bg.hover" : undefined}
        borderBottomWidth={open ? "1px" : "0"}
        borderColor="border.panel"
        _hover={{ bg: "bg.hover" }}
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <Box minW="0">
          <Text fontWeight="500" data-testid="flow-group-summary">
            <Text as="span" fontWeight="600">
              {g.label}
            </Text>
            {g.summary && ` · ${g.summary}`}
          </Text>
          <Text mt="2px" fontSize="12px" color="fg.muted" data-testid="flow-group-meta">
            {groupMeta(g)}
          </Text>
        </Box>
        <HStack as="span" gap="6px" flexShrink="0" fontSize="12px" color="accent.fg" whiteSpace="nowrap">
          {open ? "Hide" : "Show"}
          <Chevron open={open} />
        </HStack>
      </Flex>
      {open && (
        <Box px="4" ps="7">
          <Steps rows={g.rows} height={height} />
        </Box>
      )}
    </Box>
  );
}

/** The group lines while the transfers load. */
export function FlowSkeleton({ lines = 3 }: { lines?: number }) {
  return (
    <Stack gap="0" {...panel} px="4" aria-hidden data-testid="flow-skeleton">
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
    <Box as="section" mt="8" aria-labelledby="block-flow-title" data-testid="block-flow">
      <SectionHead id="block-flow-title" title="Where the funds went in this block" />
      <Text mt="-1" mb="3" fontSize="13px" color="fg.muted">
        Every coin that moved, grouped by what happened. Steps the chain does by itself are marked &ldquo;by the chain&rdquo;.
      </Text>
      {!data ? (
        error ? (
          <HStack justify="space-between" wrap="wrap" gap="3" {...panel} p="4">
            <Text fontSize="sm" color="neg.fg">
              The block&apos;s transfers could not be loaded.
            </Text>
            <Button size="sm" colorPalette="blue" onClick={() => void refetch()}>
              Try again
            </Button>
          </HStack>
        ) : (
          <FlowSkeleton />
        )
      ) : (
        <>
          <ListPanel>
            {groups.map((g) => (
              <Group key={g.group} g={g} height={height} />
            ))}
          </ListPanel>
          <Box mt="2">
            <Toggle open={all} onClick={() => setAll((o) => !o)}>
              {all ? "Hide the transfers" : `Show all ${formatCount(total)} transfers, one by one`}
            </Toggle>
          </Box>
          {all && (
            <ListPanel mt="1" px="4" data-testid="block-flow-all">
              <Steps rows={data} height={height} />
            </ListPanel>
          )}
        </>
      )}
    </Box>
  );
}
