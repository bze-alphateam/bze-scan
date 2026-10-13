"use client";

import { Box, Button, Flex, Grid, Popover, Portal, Text } from "@chakra-ui/react";

import { useStatus } from "@/api/hooks";
import type { Status } from "@/api/types";
import { formatCount, formatDateTime } from "@/lib/format";

type Light = "green" | "red" | "unknown";

const LIGHTS: Record<Light, { color: string; ring: string; text: string }> = {
  green: { color: "pos.fg", ring: "pos.subtle", text: "Healthy" },
  red: { color: "neg.fg", ring: "neg.subtle", text: "Stalled" },
  unknown: { color: "fg.muted", ring: "bg.hover", text: "Unknown" },
};

const NOTES: Record<Light, string> = {
  green: "The explorer keeps up with the chain. Checked once a minute.",
  red: "The explorer is behind the chain. The latest blocks and transactions may be missing for a moment.",
  unknown: "The explorer's status could not be read.",
};

/** Grey before the first answer and whenever the last request failed. */
function lightOf(data: Status | undefined, failed: boolean): Light {
  if (!data || failed) return "unknown";
  return data.live_fill.healthy ? "green" : "red";
}

const height = (h: number | null) => (h === null ? "—" : formatCount(h));

/** The live indexer's height, and how far behind the node it is when stalled. */
function indexerText({ db_height, node_height, healthy }: Status["live_fill"]): string {
  if (db_height === null) return "—";
  if (healthy || node_height === null || node_height <= db_height) return formatCount(db_height);
  return `${formatCount(db_height)} · ${formatCount(node_height - db_height)} blocks behind`;
}

/** The footer's health pill: whether live indexing keeps up with the chain, refetched every minute. */
export function StatusLight() {
  const { data, isError } = useStatus();
  const light = lightOf(data, isError);
  const { color, ring, text } = LIGHTS[light];

  return (
    <Popover.Root positioning={{ placement: "top-end" }}>
      <Popover.Trigger asChild>
        <Button
          variant="plain"
          size="sm"
          px="2"
          gap="1.5"
          fontWeight="600"
          color={color}
          _hover={{ bg: "bg.hover" }}
          _expanded={{ bg: "bg.hover" }}
          rounded="l1"
          data-light={light}
          aria-label={`Explorer health: ${text}`}
        >
          <Box as="span" boxSize="7px" rounded="full" bg={color} boxShadow={`0 0 0 3px var(--chakra-colors-${ring.replace(".", "-")})`} aria-hidden />
          {text}
        </Button>
      </Popover.Trigger>
      <Portal>
        <Popover.Positioner>
          <Popover.Content w="320px" maxW="calc(100vw - 32px)" p="0" bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" fontSize="12px">
            <Flex justify="space-between" align="center" px="14px" py="10px" borderBottomWidth="1px" borderColor="border.panel">
              <Popover.Title textStyle="label" fontSize="11px">
                Explorer health
              </Popover.Title>
              <Text textStyle="label" fontSize="11px" color={color}>
                {text}
              </Text>
            </Flex>
            {data && !isError && <HealthRows status={data} />}
            <Text px="14px" py="10px" borderTopWidth={data && !isError ? "1px" : "0"} borderColor="border.panel" color="fg.muted" lineHeight="1.5">
              {NOTES[light]}
            </Text>
          </Popover.Content>
        </Popover.Positioner>
      </Portal>
    </Popover.Root>
  );
}

function HealthRows({ status }: { status: Status }) {
  const live = status.live_fill;
  return (
    <Grid
      as="dl"
      templateColumns="auto minmax(0, 1fr)"
      columnGap="4"
      rowGap="2"
      px="14px"
      py="3"
      title={live.checked_at ? `Checked ${formatDateTime(live.checked_at)}` : undefined}
    >
      <Row label="Node" value={height(live.node_height)} />
      <Row label="Live indexer" value={indexerText(live)} />
      <Row label="Archive" value={height(live.archive_height)} />
      <Row label="History from block" value={height(status.back_fill.oldest_height)} />
      <Row label="Backfill" value={status.back_fill.status === "finished" ? "Done" : "Running"} mono={false} />
    </Grid>
  );
}

function Row({ label, value, mono = true }: { label: string; value: string; mono?: boolean }) {
  return (
    <>
      <Text as="dt" color="fg.muted">
        {label}
      </Text>
      <Text as="dd" textAlign="end" fontFamily={mono ? "mono" : undefined}>
        {value}
      </Text>
    </>
  );
}
