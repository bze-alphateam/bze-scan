"use client";

import { Box, Flex, Grid, Stack, Text } from "@chakra-ui/react";
import type { ReactNode } from "react";
import { LuCircleCheck, LuCircleX } from "react-icons/lu";

import type { ProposalSummary, Tally, TokenSummary, VoteOption } from "@/api/types";
import { Chip } from "@/components/ui/chip";
import { Tag, type TagTone } from "@/components/ui/status-pill";
import { formatPct } from "@/lib/format";
import { coinsText, statusInfo, tallyShares, titleStatus, type Checks, type Tone } from "@/render/proposals";

// The atoms both governance pages share: the status tag, the tally bars,
// the verdict tag, the historical chip and the three checks.

const TAG_TONES: Record<Tone, TagTone> = { blue: "accent", green: "ok", red: "bad", orange: "warn", gray: "muted" };

/** A proposal's status as a tag: VOTING, PASSED, REJECTED. */
export function ProposalStatusTag({ p, now }: { p: Pick<ProposalSummary, "status" | "deposit_end_time">; now: Date }) {
  return (
    <Tag tone={TAG_TONES[statusInfo(p, now).tone]} data-testid="proposal-status">
      {titleStatus(p, now)}
    </Tag>
  );
}

/** The colour of a vote option's bar and text. */
export const OPTION_COLORS: Record<VoteOption, string> = { yes: "pos.fg", no: "neg.fg", abstain: "fg.muted", no_with_veto: "warn.fg" };

const BARS = "64px minmax(0, 1fr) 56px";
// The amounts column shows from c2 up.
const BARS_WITH_AMOUNTS = { base: BARS, c2: "64px minmax(0, 1fr) 56px 92px" };

/** Yes, No, Abstain and Veto as 6px bars with their share of the votes cast, and the amounts when asked. */
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
    <Stack gap="7px" maxW="720px" role="list" aria-label="Votes" data-testid="tally-bars">
      {tallyShares(tally).map((s) => (
        <Grid key={s.option} role="listitem" templateColumns={amounts ? BARS_WITH_AMOUNTS : BARS} gap="3" alignItems="center" fontSize="13px" data-testid="tally-row">
          <Text>{s.label}</Text>
          <Box h="6px" bg="bg.hover" rounded="3px" overflow="hidden" aria-hidden>
            <Box h="full" w={`${Math.min(100, Number(s.pct ?? 0))}%`} bg={OPTION_COLORS[s.option]} rounded="3px" />
          </Box>
          <Text textAlign="end" fontFamily="mono">
            {s.pct != null ? formatPct(s.pct) : "—"}
          </Text>
          {amounts && (
            <Text hideBelow="c2" textAlign="end" fontFamily="mono" color="fg.muted" whiteSpace="nowrap">
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

/** The outlined "historical" chip of a turnout the snapshots command read from the archive; nothing otherwise. */
export function HistoricalChip({ p }: { p: Pick<ProposalSummary, "tally_source"> }) {
  if (p.tally_source !== "archive") return null;
  return (
    <Chip variant="hist" title={HISTORICAL_TURNOUT} tabIndex={0} data-testid="historical-turnout">
      historical
    </Chip>
  );
}

/** Where a vote is heading: ON TRACK TO PASS, or WOULD FAIL and why. */
export function VerdictTag({ v }: { v: { pass: boolean; text: string } }) {
  return (
    <Tag tone={v.pass ? "ok" : "warn"} whiteSpace="normal" data-testid="verdict">
      {v.text}
    </Tag>
  );
}

function CheckRow({ ok, title, detail, mark }: { ok: boolean; title: string; detail: string; mark?: ReactNode }) {
  return (
    <Flex as="li" gap="10px" align="flex-start" fontSize="13px" data-testid="check" data-ok={ok}>
      <Box color={ok ? "pos.fg" : "neg.fg"} pt="1px" display="inline-flex" aria-hidden>
        {ok ? <LuCircleCheck size={16} /> : <LuCircleX size={16} />}
      </Box>
      <Box minW="0">
        <Text fontWeight="500">
          {title}
          <Text as="span" srOnly>
            {ok ? ": yes" : ": no"}
          </Text>
        </Text>
        <Flex gap="1.5" wrap="wrap" align="center" mt="1px" fontSize="12px" color="fg.muted">
          <Text as="span">{detail}</Text>
          {mark}
        </Flex>
      </Box>
    </Flex>
  );
}

/** The three checks the chain runs when voting ends; the turnout marked historical when the archive measured it. */
export function ChecksList({ c, p }: { c: Checks; p?: Pick<ProposalSummary, "tally_source"> }) {
  return (
    <Stack as="ul" listStyleType="none" m="0" mt="14px" pt="3" gap="10px" borderTopWidth="1px" borderColor="border.panel" aria-label="Checks">
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
