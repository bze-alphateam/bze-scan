"use client";

import { Box, Flex, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";

import type { Token } from "@/api/types";
import { LimitBars } from "@/components/ibc/limit-bars";
import { useNow } from "@/components/ibc/use-now";
import { Card } from "@/components/ui/card";

/** The token page's "Cross-chain limits" card: one row per channel with its two bars; nothing when the token has no limits. */
export function TokenLimits({ t }: { t: Token }) {
  const now = useNow();
  const limits = t.rate_limits ?? [];
  if (limits.length === 0) return null;
  return (
    <Box mt="8" data-testid="token-limits">
      <Card title="Cross-chain limits" meta="How much of this token may cross each route in a 24-hour window.">
        <Flex direction="column" gap="5">
          {limits.map((l) => (
            <Box key={l.channel_id} data-testid="token-limit-row">
              <Text fontWeight="500" mb="2">
                {l.counterparty.pretty_name ?? l.channel_id}{" "}
                <Text as="span" color="fg.muted" fontFamily="mono" fontSize="12px">
                  {l.channel_id}
                </Text>
              </Text>
              <LimitBars limit={l} now={now} />
            </Box>
          ))}
        </Flex>
        <Text mt="4" fontSize="13px">
          <Link asChild color="accent.fg">
            <NextLink href="/ibc">All cross-chain limits →</NextLink>
          </Link>
        </Text>
      </Card>
    </Box>
  );
}
