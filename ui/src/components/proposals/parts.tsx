"use client";

import { Badge, Box, Grid, HStack, Stack, Text } from "@chakra-ui/react";
import type { ReactNode } from "react";
import { LuCircleCheck, LuCircleX } from "react-icons/lu";

import type { ProposalSummary, Tally, TokenSummary } from "@/api/types";
import { formatPct } from "@/lib/format";
import { coinsText, statusInfo, tallyShares, type Checks } from "@/render/proposals";

// The atoms both governance pages share: the status pill, the tally bars,
// the verdict chip and the three checks.

export function ProposalStatusBadge({ p, now }: { p: Pick<ProposalSummary, "status" | "deposit_end_time">; now: Date }) {
  const s = statusInfo(p, now);
  return (
    <Badge colorPalette={s.tone} variant="subtle" size="md" rounded="full" data-testid="proposal-status">
      {s.text}
    </Badge>
  );
}

/** Yes, No, Abstain and Veto as bars with their share of the votes cast, and the amounts when asked. */
export function TallyBars({
  tally,
  denoms,
  amounts = false,
}: {
  tally: Tally | null;
  denoms?: Map<string, TokenSummary>;
  amounts?: boolean;
}) {
  return (
    <Stack gap="2" role="list" aria-label="Votes" data-testid="tally-bars">
      {tallyShares(tally).map((s) => (
        <Grid
          key={s.option}
          role="listitem"
          templateColumns={{ base: "64px minmax(0, 1fr) 56px", md: amounts ? "72px minmax(0, 1fr) 56px 128px" : "72px minmax(0, 1fr) 56px" }}
          gap="3"
          alignItems="center"
          fontSize="sm"
          data-testid="tally-row"
        >
          <Text>{s.label}</Text>
          <Box h="2" bg="bg.muted" rounded="full" overflow="hidden" aria-hidden>
            <Box h="full" w={`${Math.min(100, Number(s.pct ?? 0))}%`} bg={`${s.tone}.solid`} rounded="full" />
          </Box>
          <Text textAlign="end" fontVariantNumeric="tabular-nums">
            {s.pct != null ? formatPct(s.pct) : "—"}
          </Text>
          {amounts && (
            <Text hideBelow="md" textAlign="end" color="fg.muted" fontVariantNumeric="tabular-nums">
              {coinsText([{ denom: "ubze", amount: s.amount }], denoms, true)}
            </Text>
          )}
        </Grid>
      ))}
    </Stack>
  );
}

const HISTORICAL_TURNOUT =
  "Read from the chain's state at the end of voting, after the fact: the explorer was not running when this proposal was voted on.";

/** The muted "historical" mark of a turnout the snapshots command read from the archive; nothing otherwise. */
export function HistoricalChip({ p }: { p: Pick<ProposalSummary, "tally_source"> }) {
  if (p.tally_source !== "archive") return null;
  return (
    <Badge variant="outline" colorPalette="gray" size="sm" rounded="full" color="fg.muted" title={HISTORICAL_TURNOUT} data-testid="historical-turnout">
      historical
    </Badge>
  );
}

export function VerdictChip({ v }: { v: { pass: boolean; text: string } }) {
  return (
    <Badge colorPalette={v.pass ? "green" : "orange"} variant="subtle" size="md" rounded="full" data-testid="verdict">
      {v.text}
    </Badge>
  );
}

function CheckRow({ ok, title, detail, mark }: { ok: boolean; title: string; detail: string; mark?: ReactNode }) {
  return (
    <HStack gap="2" align="start" role="listitem" data-testid="check" data-ok={ok}>
      <Box color={ok ? "green.fg" : "red.fg"} pt="0.5" aria-hidden>
        {ok ? <LuCircleCheck /> : <LuCircleX />}
      </Box>
      <Stack gap="0">
        <Text fontSize="sm" fontWeight="medium">
          {title}
          <Text as="span" srOnly>
            {ok ? ": yes" : ": no"}
          </Text>
        </Text>
        <HStack gap="1.5" wrap="wrap" fontSize="xs" color="fg.muted">
          <Text as="span">{detail}</Text>
          {mark}
        </HStack>
      </Stack>
    </HStack>
  );
}

/** The three checks the chain runs when voting ends; the turnout marked historical when the archive measured it. */
export function ChecksList({ c, p }: { c: Checks; p?: Pick<ProposalSummary, "tally_source"> }) {
  return (
    <Stack gap="2" role="list" aria-label="Checks">
      <CheckRow
        ok={c.turnout.ok}
        title="Enough people voted"
        detail={`${formatPct(c.turnout.value)} of staked BZE voted · ${formatPct(c.turnout.need, { trim: true })} needed`}
        mark={p && <HistoricalChip p={p} />}
      />
      <CheckRow
        ok={c.yes.ok}
        title="Enough Yes votes"
        detail={`${formatPct(c.yes.value)} Yes, not counting Abstain · more than ${formatPct(c.yes.need, { trim: true })} needed`}
      />
      <CheckRow
        ok={c.veto.ok}
        title="Not vetoed"
        detail={`${formatPct(c.veto.value)} No with veto · at most ${formatPct(c.veto.need, { trim: true })} allowed`}
      />
    </Stack>
  );
}
