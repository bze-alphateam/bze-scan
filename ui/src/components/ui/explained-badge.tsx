"use client";

import { Box, HStack, Image, Popover, Portal, Text } from "@chakra-ui/react";

import type { ExplainedBadge as Explained } from "@/render/badges";

import { Chip } from "./chip";

/**
 * A badge, drawn as a chip, that says what it means: the explainer is its tooltip on hover and
 * a popover on tap (phones have no hover).
 */
export function ExplainedBadge({ badge, size = "sm" }: { badge: Explained; size?: "sm" | "md" }) {
  return (
    <Popover.Root positioning={{ placement: "top" }} lazyMount unmountOnExit>
      <Popover.Trigger asChild>
        <Box asChild maxW="full" cursor="help" textAlign="start">
          <button type="button" title={badge.explainer} aria-label={`${badge.text}: ${badge.explainer}`} data-testid={`${badge.kind}-tag`}>
            <Chip variant={badge.look} whiteSpace="normal" fontSize={size === "md" ? "12px" : "11px"}>
              {badge.logoUrl && <Image src={badge.logoUrl} alt="" boxSize="3" rounded="full" />}
              {badge.text}
            </Chip>
          </button>
        </Box>
      </Popover.Trigger>
      <Portal>
        <Popover.Positioner>
          <Popover.Content maxW="xs">
            <Popover.Arrow />
            <Popover.Body>
              <Text fontSize="sm">{badge.explainer}</Text>
            </Popover.Body>
          </Popover.Content>
        </Popover.Positioner>
      </Portal>
    </Popover.Root>
  );
}

/** A row of explained badges, wrapping. */
export function ExplainedBadges({ badges }: { badges: Explained[] }) {
  return (
    <HStack gap="1" minW="0" wrap="wrap" data-testid="token-badges">
      {badges.map((b) => (
        <ExplainedBadge key={b.kind} badge={b} />
      ))}
    </HStack>
  );
}
