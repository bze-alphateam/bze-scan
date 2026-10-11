"use client";

import { Box, Button, Popover, Portal, Stack, Text } from "@chakra-ui/react";

import { useStatus } from "@/api/hooks";
import type { Status } from "@/api/types";
import { formatCount, formatDateTime, relativeTime } from "@/lib/format";

type Light = "green" | "red" | "unknown";

const LIGHTS: Record<Light, { color: string; text: string }> = {
  green: { color: "green.500", text: "Up to date" },
  red: { color: "red.500", text: "Behind" },
  unknown: { color: "gray.500", text: "Unknown" },
};

/** Grey before the first answer and whenever the last request failed. */
function lightOf(data: Status | undefined, failed: boolean): Light {
  if (!data || failed) return "unknown";
  return data.live_fill.healthy ? "green" : "red";
}

const height = (h: number | null) => (h === null ? "—" : formatCount(h));

function historyLine(status: Status): string | null {
  const { oldest_height, status: state } = status.back_fill;
  if (oldest_height === null) return null;
  const from = `History from block ${formatCount(oldest_height)}`;
  return state === "finished" ? from : `${from}, older blocks still loading`;
}

/** The footer's light: whether live indexing keeps up with the chain, refetched every minute. */
export function StatusLight() {
  const { data, isError } = useStatus();
  const light = lightOf(data, isError);
  const { color, text } = LIGHTS[light];

  return (
    <Popover.Root positioning={{ placement: "top" }}>
      <Popover.Trigger asChild>
        <Button variant="ghost" size="sm" minH="44px" data-light={light} aria-label={`Explorer status: ${text}`}>
          <Box as="span" boxSize="2.5" rounded="full" bg={color} aria-hidden />
          {text}
        </Button>
      </Popover.Trigger>
      <Portal>
        <Popover.Positioner>
          <Popover.Content maxW="xs">
            <Popover.Arrow />
            <Popover.Body>
              <Popover.Title fontWeight="semibold" mb="2">
                {text}
              </Popover.Title>
              {data && !isError ? (
                <StatusDetails status={data} />
              ) : (
                <Text fontSize="sm" color="fg.muted">
                  The explorer&apos;s status could not be read.
                </Text>
              )}
            </Popover.Body>
          </Popover.Content>
        </Popover.Positioner>
      </Portal>
    </Popover.Root>
  );
}

function StatusDetails({ status }: { status: Status }) {
  const live = status.live_fill;
  const history = historyLine(status);
  return (
    <Stack gap="1" fontSize="sm">
      <Row label="Explorer height" value={height(live.db_height)} />
      <Row label="Node height" value={height(live.node_height)} />
      <Row label="Archive height" value={height(live.archive_height)} />
      <Text color="fg.muted" title={live.checked_at ? formatDateTime(live.checked_at) : undefined}>
        {live.checked_at ? `checked ${relativeTime(live.checked_at)}` : "not checked yet"}
      </Text>
      {history && <Text>{history}</Text>}
    </Stack>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <Box display="flex" justifyContent="space-between" gap="4">
      <Text color="fg.muted">{label}</Text>
      <Text fontVariantNumeric="tabular-nums">{value}</Text>
    </Box>
  );
}
