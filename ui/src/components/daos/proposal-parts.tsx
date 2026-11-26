"use client";

import { Box, Flex, Grid, Text } from "@chakra-ui/react";

import type { DaoProposalSummary, DaoWeightDenom } from "@/api/types";
import { OPTION_COLORS } from "@/components/proposals/parts";
import { Tag } from "@/components/ui/status-pill";
import { formatPct } from "@/lib/format";
import { shares, statusInfo, weightText } from "@/render/dao-proposals";

/** A DAO proposal's status as a tag: VOTING, PASSED (EXECUTED), REJECTED… */
export function DaoStatusTag({ status }: { status: string | null }) {
  const s = statusInfo(status);
  return (
    <Tag tone={s.tone} data-testid="dao-proposal-status" data-status={status ?? "none"}>
      {s.label}
    </Tag>
  );
}

const BARS = "64px minmax(0, 1fr) 56px";
// The amounts column shows from c2 up.
const BARS_WITH_AMOUNTS = { base: BARS, c2: "64px minmax(0, 1fr) 56px 120px" };

const OPTIONS = [
  { key: "yes", label: "Yes" },
  { key: "no", label: "No" },
  { key: "abstain", label: "Abstain" },
] as const;

type Votes = Pick<DaoProposalSummary, "yes" | "no" | "abstain" | "denominator">;

/** Yes, No and Abstain as 6px bars, each as a share of the voting power pinned at submission; the amounts when asked. */
export function DaoTallyBars({ p, weight, stake = false, amounts = false }: { p: Votes; weight?: DaoWeightDenom | null; stake?: boolean; amounts?: boolean }) {
  const s = shares(p);
  return (
    <Flex direction="column" gap="7px" maxW="720px" role="list" aria-label="Votes" data-testid="dao-tally-bars">
      {OPTIONS.map((o) => (
        <Grid key={o.key} role="listitem" templateColumns={amounts ? BARS_WITH_AMOUNTS : BARS} gap="3" alignItems="center" fontSize="13px" data-testid="dao-tally-row" data-option={o.key}>
          <Text>{o.label}</Text>
          <Box h="6px" bg="bg.hover" rounded="3px" overflow="hidden" aria-hidden>
            <Box h="full" w={`${Math.min(100, Number(s[o.key] ?? 0))}%`} bg={OPTION_COLORS[o.key]} rounded="3px" />
          </Box>
          <Text textAlign="end" fontFamily="mono">
            {s[o.key] != null ? formatPct(s[o.key]!) : "—"}
          </Text>
          {amounts && (
            <Text hideBelow="c2" textAlign="end" fontFamily="mono" color="fg.muted" whiteSpace="nowrap">
              {weightText(p[o.key], weight, true, stake)}
            </Text>
          )}
        </Grid>
      ))}
    </Flex>
  );
}
