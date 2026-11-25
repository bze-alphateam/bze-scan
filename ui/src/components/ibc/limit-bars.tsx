"use client";

import { Box, Flex, SimpleGrid, Text } from "@chakra-ui/react";

import type { RateLimit } from "@/api/types";
import { directionView, resetText, type BarTone, type DirectionView } from "@/render/rate-limits";

const FILL: Record<BarTone, string> = { ok: "accent.fg", warn: "warn.fg", bad: "neg.fg" };

function Direction({ view }: { view: DirectionView }) {
  return (
    <Box minW="0" data-testid={`limit-${view.label.toLowerCase()}`}>
      <Flex gap="2" align="baseline" wrap="wrap">
        <Text as="span" textStyle="label" minW="28px">
          {view.label}
        </Text>
        <Text as="span" fontSize="13px" color={view.kind === "bar" ? undefined : "fg.muted"} wordBreak="break-word">
          {view.text}
        </Text>
      </Flex>
      {view.kind === "bar" && (
        <>
          <Box mt="6px" h="6px" rounded="full" bg="bg.hover" overflow="hidden">
            <Box
              role="progressbar"
              aria-label={`${view.label} used`}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={view.pct}
              data-tone={view.tone}
              h="full"
              rounded="full"
              bg={FILL[view.tone]}
              style={{ width: `${view.pct}%` }}
            />
          </Box>
          {view.note && (
            <Text mt="4px" fontSize="12px" color="fg.muted">
              {view.note}
            </Text>
          )}
        </>
      )}
    </Box>
  );
}

/** A limit's two directions: Out and In side by side, stacked on a phone, with the reset line under them. */
export function LimitBars({ limit, now }: { limit: RateLimit; now?: Date }) {
  return (
    <Box minW="0">
      <SimpleGrid columns={{ base: 1, md: 2 }} gap={{ base: "3", md: "6" }}>
        <Direction view={directionView(limit, "send")} />
        <Direction view={directionView(limit, "recv")} />
      </SimpleGrid>
      <Text mt="6px" fontSize="12px" color="fg.muted" data-testid="limit-reset">
        {resetText(limit, now)}
      </Text>
    </Box>
  );
}
