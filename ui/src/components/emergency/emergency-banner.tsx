"use client";

import { Box, Container, Flex, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { LuTriangleAlert } from "react-icons/lu";

import { useEmergency, useLiveHalt } from "@/api/hooks";
import { bannerParts, bannerText } from "@/render/emergency";

/**
 * The thin orange bar under the header (and under the pause strip) while an
 * emergency measure is in force. Never shown when none is, nor when the
 * state cannot be read. An emergency chain halt that has stopped the chain
 * is the pause strip's, so it is not repeated here.
 */
export function EmergencyBanner() {
  const emergency = useEmergency();
  const halt = useLiveHalt().data;
  const e = emergency.data;
  if (emergency.isError || !e?.supported || !e.active) return null;
  const paused = halt?.halted === true && halt.kind === "emergency";
  const parts = bannerParts(e, paused);
  if (parts.length === 0) return null;
  return (
    <Box role="status" bg="warn.subtle" borderBottomWidth="1px" borderColor="border.panel" data-testid="emergency-banner">
      <Container maxW="7xl" px={{ base: "4", md: "6" }} py="2">
        <Flex gap="2.5" align="flex-start" fontSize="13px" lineHeight="1.5">
          <Box as="span" color="warn.fg" mt="2px" flexShrink={0} aria-hidden>
            <LuTriangleAlert size={16} />
          </Box>
          <Text>
            <Text as="span" fontWeight="600">
              Emergency measures active:
            </Text>{" "}
            {bannerText(parts)}{" "}
            <Link asChild color="accent.fg" fontWeight="600">
              <NextLink href="/emergency">details →</NextLink>
            </Link>
          </Text>
        </Flex>
      </Container>
    </Box>
  );
}
