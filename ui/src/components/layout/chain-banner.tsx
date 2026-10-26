"use client";

import { Box, Container, Flex, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import type { ReactNode } from "react";
import { LuClock, LuPause } from "react-icons/lu";

import { useLiveHalt, useStatus } from "@/api/hooks";
import { backLine, behindLine, haltedFor, haltProgress } from "@/render/halt";

/**
 * The strip under the header: the chain paused for a software upgrade (with
 * how much voting power is back), or else the explorer behind the chain. A
 * stall that is not an upgrade's halt only ever reads as the explorer being
 * behind, so nobody takes it for the chain stopping. Nothing while all is well.
 */
export function ChainBanner() {
  const halt = useLiveHalt().data;
  const status = useStatus().data;

  if (halt?.halted) {
    const p = haltProgress(halt);
    return (
      <Strip tone="warn" icon={<LuPause size={16} />} testId="halt-banner">
        <Text as="span" fontWeight="600">
          The chain is paused for the {halt.name} upgrade
        </Text>{" "}
        (for {haltedFor(halt, new Date())}). {backLine(p)} on the new version.{" "}
        <Link asChild color="accent.fg" fontWeight="600">
          <NextLink href={`/proposals/${halt.proposal_id}`}>See who is missing</NextLink>
        </Link>
      </Strip>
    );
  }
  const behind = behindLine(status);
  if (!behind) return null;
  return (
    <Strip tone="muted" icon={<LuClock size={16} />} testId="behind-banner">
      {behind}
    </Strip>
  );
}

function Strip({ tone, icon, testId, children }: { tone: "warn" | "muted"; icon: ReactNode; testId: string; children: ReactNode }) {
  return (
    <Box
      role="status"
      bg={tone === "warn" ? "warn.subtle" : "bg.subtle"}
      borderBottomWidth="1px"
      borderColor="border.panel"
      data-testid={testId}
    >
      <Container maxW="7xl" px={{ base: "4", md: "6" }} py="2.5">
        <Flex gap="2.5" align="flex-start" fontSize="13px" lineHeight="1.5">
          <Box as="span" color={tone === "warn" ? "warn.fg" : "fg.muted"} mt="2px" flexShrink={0} aria-hidden>
            {icon}
          </Box>
          <Text>{children}</Text>
        </Flex>
      </Container>
    </Box>
  );
}
