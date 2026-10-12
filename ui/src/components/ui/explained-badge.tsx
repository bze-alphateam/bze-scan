"use client";

import { Badge, HStack, Image, Popover, Portal, Text } from "@chakra-ui/react";

import type { ExplainedBadge as Explained } from "@/render/badges";

/**
 * A badge that says what it means: the explainer is its tooltip on hover and
 * a popover on tap (phones have no hover).
 */
export function ExplainedBadge({ badge, size = "sm" }: { badge: Explained; size?: "sm" | "md" }) {
  return (
    <Popover.Root positioning={{ placement: "top" }}>
      <Popover.Trigger asChild>
        <Badge
          as="button"
          colorPalette={badge.tone}
          variant={badge.variant}
          size={size}
          maxW="full"
          h="auto"
          py="0.5"
          whiteSpace="normal"
          textAlign="start"
          cursor="help"
          title={badge.explainer}
          aria-label={`${badge.text}: ${badge.explainer}`}
          data-testid={`${badge.kind}-tag`}
        >
          {badge.logoUrl && <Image src={badge.logoUrl} alt="" boxSize="3" rounded="full" />}
          {badge.text}
        </Badge>
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
