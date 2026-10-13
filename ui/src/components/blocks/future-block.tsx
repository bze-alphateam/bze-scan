"use client";

import { Box, Button, Flex, Grid, HStack, IconButton, Input, Link, Skeleton, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { LuArrowRight, LuArrowUp, LuCalendar, LuTriangleAlert } from "react-icons/lu";

import { useProposalList, useProposals, useStats } from "@/api/hooks";
import type { ProposalSummary } from "@/api/types";
import { Chip } from "@/components/ui/chip";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { Breadcrumb, DetailTitle, PanelHead } from "@/components/ui/page-head";
import { StateBox } from "@/components/ui/state-box";
import { VTabPanel, VTabs, VTabsBar } from "@/components/ui/vtabs";
import { formatCount, formatDate, formatDateTime } from "@/lib/format";
import {
  byDate,
  byHeight,
  countdown,
  coversText,
  dateInputs,
  dateParam,
  durationText,
  estimator,
  formatLocal,
  parseDateTime,
  parseHeight,
  tipAt,
  type Estimator,
} from "@/render/future-block";
import { describeProposal } from "@/render/proposals";

// Custom breakpoints are not in Chakra's generated types, so responsive objects using them are hoisted.
// The two panels side by side from c2, stacked below.
const PANELS = { base: "minmax(0, 1fr)", c2: "minmax(0, 1fr) minmax(0, 1fr)" };
// Average over the last · Block time · Covers (c2).
const WINDOW_COLUMNS = { base: "minmax(0, 1fr) auto", c2: "minmax(0, 1fr) auto 90px" };
const CALC_PAD = { base: "4", c2: "20px 24px 24px" };
const UNIT_FONT = { base: "30px", c2: "46px" };
const UNIT_W = { base: "0", c2: "92px" };
const UNIT_FLEX = { base: "1", c2: "none" };
const UNIT_PX = { base: "1", c2: "10px" };
const UNIT_PT = { base: "10px", c2: "14px" };
const UNIT_PB = { base: "2", c2: "10px" };
// The four boxes fill the width on phones.
const COUNTDOWN_GAP = { base: "6px", c2: "10px" };
const COUNTDOWN_W = { base: "full", c2: "auto" };
const HEIGHT_FONT = { base: "36px", c2: "52px" };
const SEPARATOR = { base: "none", c2: "block" };
const LABEL_W = { base: "full", c2: "110px" };

type Mode = "height" | "date";

/** The current time, moved every second: the countdown's clock. */
function useNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  return now;
}

/** An input row: the label, the bordered box with its inputs and the arrow button, the hint. */
function InputRow({ label, htmlFor, hint, children, w = "320px" }: { label: string; htmlFor: string; hint: ReactNode; children: ReactNode; w?: string }) {
  return (
    <Flex as="form" align="center" gap="14px" wrap="wrap" onSubmit={(e: FormEvent) => e.preventDefault()}>
      <Text as="label" {...{ htmlFor }} fontSize="13px" fontWeight="500" minW={LABEL_W}>
        {label}
      </Text>
      <HStack
        gap="2"
        w={w}
        maxW="full"
        h="40px"
        ps="14px"
        pe="6px"
        bg="bg.page"
        borderWidth="1px"
        borderColor="border.panel"
        rounded="l2"
        _focusWithin={{ borderColor: "accent.fg" }}
      >
        {children}
        <IconButton
          type="submit"
          aria-label="Estimate"
          variant="outline"
          flex="none"
          boxSize="28px"
          minW="28px"
          h="28px"
          rounded="l1"
          color="fg.muted"
          borderColor="border.panel"
          _hover={{ color: "accent.fg", borderColor: "accent.fg", bg: "transparent" }}
        >
          <LuArrowRight />
        </IconButton>
      </HStack>
      <Text fontSize="12.5px" color="fg.muted">
        {hint}
      </Text>
    </Flex>
  );
}

function TextInput(props: { id: string; value: string; onChange: (v: string) => void; placeholder?: string; label?: string; flex?: string }) {
  return (
    <Input
      id={props.id}
      aria-label={props.label}
      inputMode="numeric"
      autoComplete="off"
      placeholder={props.placeholder}
      value={props.value}
      onChange={(e) => props.onChange(e.target.value)}
      unstyled
      flex={props.flex ?? "1"}
      minW="0"
      h="full"
      bg="transparent"
      color="fg"
      fontFamily="mono"
      outline="none"
      _placeholder={{ color: "fg.muted" }}
    />
  );
}

/** The result area under the inputs: a line above, centred. */
function Result({ children, testId }: { children: ReactNode; testId: string }) {
  return (
    <Stack align="center" textAlign="center" gap="0" mt="5" pt="6" borderTopWidth="1px" borderColor="border.panel" data-testid={testId}>
      {children}
    </Stack>
  );
}

function Message({ children, muted }: { children: ReactNode; muted?: boolean }) {
  return (
    <Box mt="5" pt="18px" borderTopWidth="1px" borderColor="border.panel" fontSize="14px" color={muted ? "fg.muted" : undefined} data-testid="future-message">
      {children}
    </Box>
  );
}

const kicker = { fontSize: "11px", fontWeight: "600", letterSpacing: "0.1em", textTransform: "uppercase", color: "fg.muted" } as const;

function Unit({ value, label }: { value: string; label: string }) {
  return (
    <Stack
      align="center"
      gap="0"
      flex={UNIT_FLEX}
      minW={UNIT_W}
      px={UNIT_PX}
      pt={UNIT_PT}
      pb={UNIT_PB}
      borderWidth="1px"
      borderColor="border.panel"
      rounded="l2"
      bg="bg.page"
    >
      <Text fontFamily="mono" fontSize={UNIT_FONT} fontWeight="500" lineHeight="1" letterSpacing="-0.02em" data-testid={`countdown-${label}`}>
        {value}
      </Text>
      <Text as="small" mt="2" fontSize="11px" letterSpacing="0.06em" textTransform="uppercase" color="fg.muted">
        {label}
      </Text>
    </Stack>
  );
}

function Colon() {
  return (
    <Text as="span" display={SEPARATOR} fontFamily="mono" fontSize="36px" color="fg.muted" pt="4" aria-hidden>
      :
    </Text>
  );
}

function ByHeight({ e, now, text, onText }: { e: Estimator; now: number; text: string; onText: (v: string) => void }) {
  const tip = tipAt(e, now);
  const height = parseHeight(text);
  const estimate = height != null ? byHeight(e, height, now) : null;
  return (
    <>
      <InputRow
        label="Block height"
        htmlFor="future-height"
        hint={
          <>
            Latest block{" "}
            <Link asChild color="accent.fg" fontFamily="mono">
              <NextLink href="/blocks">{formatCount(tip)}</NextLink>
            </Link>
          </>
        }
      >
        <TextInput id="future-height" value={text} onChange={onText} />
      </InputRow>
      {height == null ? (
        <Message muted>
          Type a block height, like{" "}
          <Text as="span" fontFamily="mono">
            26,000,000
          </Text>
          .
        </Message>
      ) : estimate?.kind === "past" ? (
        <Message>
          Block{" "}
          <Text as="span" fontFamily="mono">
            {formatCount(height)}
          </Text>{" "}
          is already produced.{" "}
          <Link asChild color="accent.fg">
            <NextLink href={`/blocks/${height}`}>Open block {formatCount(height)}</NextLink>
          </Link>
        </Message>
      ) : estimate?.kind === "future" ? (
        <Countdown height={height} left={estimate.left} eta={estimate.eta} marginMs={estimate.marginMs} now={now} />
      ) : null}
    </>
  );
}

function Countdown({ height, left, eta, marginMs, now }: { height: number; left: number; eta: number; marginMs: number; now: number }) {
  const c = countdown(eta - now);
  return (
    <Result testId="future-countdown">
      <Text {...kicker}>
        Block{" "}
        <Text as="span" fontFamily="mono">
          {formatCount(height)}
        </Text>{" "}
        in about
      </Text>
      <HStack gap={COUNTDOWN_GAP} align="flex-start" mt="14px" w={COUNTDOWN_W} role="timer">
        <Unit value={c.days} label="days" />
        <Colon />
        <Unit value={c.hours} label="hours" />
        <Colon />
        <Unit value={c.minutes} label="minutes" />
        <Colon />
        <Unit value={c.seconds} label="seconds" />
      </HStack>
      <HStack gap="2" mt="18px" fontSize="14px" wrap="wrap" justify="center">
        <Box as="span" color="accent.fg" display="inline-flex" aria-hidden>
          <LuCalendar size={16} />
        </Box>
        <Text as="span" data-testid="future-when">
          Around <b>{formatDateTime(new Date(eta), { seconds: false })}</b>{" "}
          <Text as="span" color="fg.muted">
            · {formatLocal(eta)} your time
          </Text>
        </Text>
      </HStack>
      <HStack gap="18px" mt="10px" fontSize="13px" color="fg.muted" wrap="wrap" justify="center" data-testid="future-left">
        <Text as="span">
          <Text as="span" fontFamily="mono">
            {formatCount(left)}
          </Text>{" "}
          blocks to go
        </Text>
        <Text as="span">
          give or take{" "}
          <Text as="span" fontFamily="mono">
            {durationText(marginMs)}
          </Text>
        </Text>
      </HStack>
    </Result>
  );
}

function ByDate({
  e,
  now,
  date,
  time,
  onDate,
  onTime,
  onCountDown,
}: {
  e: Estimator;
  now: number;
  date: string;
  time: string;
  onDate: (v: string) => void;
  onTime: (v: string) => void;
  onCountDown: (height: number) => void;
}) {
  const at = parseDateTime(date, time);
  const estimate = at != null ? byDate(e, at, now) : null;
  return (
    <>
      <InputRow
        label="Date and time"
        htmlFor="future-date"
        w="340px"
        hint={
          <>
            day.month.year · 24-hour · your time zone · now{" "}
            <Text as="span" fontFamily="mono">
              {formatLocal(now)}
            </Text>
          </>
        }
      >
        <TextInput id="future-date" label="Date, day.month.year" placeholder="dd.mm.yyyy" value={date} onChange={onDate} flex="1.4" />
        <Box w="1px" alignSelf="stretch" my="2" bg="border.panel" aria-hidden />
        <TextInput id="future-time" label="Time, 24-hour" placeholder="hh:mm" value={time} onChange={onTime} />
      </InputRow>
      {at == null ? (
        <Message>Type the date as day.month.year and the time as 24-hour, like 18.10.2026 and 05:00.</Message>
      ) : estimate == null ? (
        <Message>
          That date is in the past. Past blocks are on the{" "}
          <Link asChild color="accent.fg">
            <NextLink href="/blocks">Blocks page</NextLink>
          </Link>
          .
        </Message>
      ) : (
        <Result testId="future-date-result">
          <Text {...kicker}>
            At {formatLocal(at)} ({formatDateTime(new Date(at), { seconds: false })}) the chain should be at about
          </Text>
          <Text fontFamily="mono" fontSize={HEIGHT_FONT} fontWeight="500" letterSpacing="-0.02em" mt="3" color="accent.fg" lineHeight="1.2" data-testid="future-height">
            {formatCount(estimate.height)}
          </Text>
          <HStack gap="18px" mt="10px" fontSize="13px" color="fg.muted" wrap="wrap" justify="center">
            <Text as="span">
              give or take{" "}
              <Text as="span" fontFamily="mono">
                {formatCount(estimate.marginBlocks)}
              </Text>{" "}
              blocks
            </Text>
            <Text as="span">
              in{" "}
              <Text as="span" fontFamily="mono">
                {durationText(estimate.inMs)}
              </Text>
            </Text>
          </HStack>
          <Button variant="outline" size="sm" mt="3" onClick={() => onCountDown(estimate.height)}>
            Count down to block {formatCount(estimate.height)}
          </Button>
        </Result>
      )}
    </>
  );
}

/** "ends Oct 16" (UTC). */
const endsOn = (end: string) => `ends ${formatDate(end).replace(/, \d{4}$/, "")}`;

interface Planned {
  p: ProposalSummary;
  name: string;
  height: number;
}

/** The software upgrades of passed or voting proposals whose height is still ahead. */
function usePlanned(tip: number | null): { items: Planned[]; loading: boolean } {
  const voting = useProposals("voting_period");
  const passed = useProposals("passed");
  // The mock API and stale pages may hand back other statuses: keep only the asked one.
  const candidates = [
    ...(voting.data?.pages[0]?.items ?? []).filter((p) => p.status === "voting_period"),
    ...(passed.data?.pages[0]?.items ?? []).filter((p) => p.status === "passed"),
  ].filter((p, i, all) => p.kind === "software_upgrade" && all.findIndex((q) => q.id === p.id) === i);
  const details = useProposalList(candidates.map((p) => p.id));
  const items: Planned[] = [];
  candidates.forEach((p, i) => {
    const detail = details[i]?.data;
    if (!detail) return;
    for (const a of describeProposal(detail)) {
      if (a.type === "upgrade" && a.height != null && tip != null && a.height > tip) items.push({ p, name: a.name, height: a.height });
    }
  });
  items.sort((a, b) => a.height - b.height);
  const loading = (!voting.data && !voting.error) || (!passed.data && !passed.error) || details.some((d) => d.isPending && !d.isError);
  return { items, loading };
}

function PlannedBlocks({ tip, onCountDown }: { tip: number | null; onCountDown: (height: number) => void }) {
  const { items, loading } = usePlanned(tip);
  return (
    <ListPanel as="section" aria-labelledby="future-planned">
      <PanelHead id="future-planned" title="Planned blocks" />
      {items.length === 0 ? (
        loading ? (
          <Box px="4" py="14px" aria-hidden>
            <Skeleton h="4" w="70%" />
          </Box>
        ) : (
          <Text px="4" py="14px" fontSize="13px" color="fg.muted" data-testid="planned-empty">
            No upgrade is planned right now.
          </Text>
        )
      ) : (
        <Box role="list" aria-label="Planned blocks">
          {items.map((u) => (
            <HStack key={`${u.p.id}:${u.height}`} role="listitem" gap="3" px="4" py="14px" _hover={{ bg: "bg.hover" }} data-testid="planned-row">
              <Box display="grid" placeItems="center" boxSize="40px" flex="none" rounded="l2" bg="accent.subtle" color="accent.fg" aria-hidden>
                <LuArrowUp size={16} />
              </Box>
              <Box flex="1" minW="0">
                <Link asChild display="block" fontWeight="600" color="accent.fg">
                  <NextLink href={`/proposals/${u.p.id}`}>Upgrade to {u.name}</NextLink>
                </Link>
                <Text fontSize="13px" color="fg.muted">
                  at block{" "}
                  <Text as="span" fontFamily="mono">
                    {formatCount(u.height)}
                  </Text>{" "}
                  · proposal{" "}
                  <Text as="span" fontFamily="mono">
                    #{u.p.id}
                  </Text>
                  ,{" "}
                  {u.p.status === "voting_period"
                    ? `voting${u.p.voting_end_time ? `, ${endsOn(u.p.voting_end_time)}` : ""} · happens only if it passes`
                    : "passed"}
                </Text>
              </Box>
              <Button variant="outline" size="sm" flex="none" onClick={() => onCountDown(u.height)}>
                Count down
              </Button>
            </HStack>
          ))}
        </Box>
      )}
      <Text px="4" pb="14px" pt={items.length === 0 ? "0" : "1"} fontSize="13px" color="fg.muted">
        Upgrades from passed or voting proposals are listed here.
      </Text>
    </ListPanel>
  );
}

const seconds = (ms: number) => `${(ms / 1000).toFixed(2)} s`;

function HowItWorks({ e, windows }: { e: Estimator; windows: { blocks: number; avg_block_time_ms: number | null }[] }) {
  return (
    <ListPanel as="section" aria-labelledby="future-how">
      <PanelHead id="future-how" title="How the estimate works" />
      {windows.length > 0 && (
        <>
          <ListHead templateColumns={WINDOW_COLUMNS} aria-hidden>
            <Text>Average over the last</Text>
            <Text textAlign="right">Block time</Text>
            <Text hideBelow="c2" textAlign="right">
              Covers
            </Text>
          </ListHead>
          <Box role="list" aria-label="Block time averages">
            {windows.map((w) => {
              const used = w.blocks === e.window;
              return (
                <ListRow key={w.blocks} role="listitem" templateColumns={WINDOW_COLUMNS} fontSize="13px" data-testid="window-row" data-used={used || undefined}>
                  <HStack gap="2">
                    <Text as="span">
                      <Text as="span" fontFamily="mono">
                        {formatCount(w.blocks)}
                      </Text>{" "}
                      blocks
                    </Text>
                    {used && <Chip variant="acc">used</Chip>}
                  </HStack>
                  <Text textAlign="right" fontFamily="mono">
                    {w.avg_block_time_ms != null ? seconds(w.avg_block_time_ms) : "—"}
                  </Text>
                  <Text hideBelow="c2" textAlign="right" color="fg.muted">
                    {w.avg_block_time_ms != null ? coversText(w.blocks, w.avg_block_time_ms) : "—"}
                  </Text>
                </ListRow>
              );
            })}
          </Box>
        </>
      )}
      <Text px="4" py="14px" fontSize="13px" lineHeight="1.55" color="fg.muted" data-testid="future-how-note">
        {e.window != null ? `The estimate uses the ${formatCount(e.window)}-block average.` : "The estimate uses the average block time."}{" "}
        The “give or take” is how far the averages differ (
        <Text as="span" fontFamily="mono">
          {seconds(e.spread * e.bt)}
        </Text>
        , about{" "}
        <Text as="span" fontFamily="mono">
          {(e.spread * 100).toFixed(1)}%
        </Text>
        ), applied to the time left.
      </Text>
    </ListPanel>
  );
}

export interface EstimatorProps {
  /** The By block height input's first text. */
  height?: string;
  /** `?date=YYYY-MM-DDTHH:MM`: opens the By date tab with it. */
  date?: string;
}

/** The tabs, the calculator and the two panels: the page's body, also shown in place on a block not produced yet. */
export function FutureBlockEstimator({ height = "", date }: EstimatorProps) {
  const stats = useStats();
  const now = useNow();
  const fromParam = date ? dateParam(date) : null;
  const [mode, setMode] = useState<Mode>(fromParam ? "date" : "height");
  const [heightText, setHeightText] = useState(height);
  // The By date inputs start five days ahead, at the top of the hour.
  const [dateText, setDateText] = useState(() => fromParam?.date ?? dateInputs(Date.now() + 5 * 86_400_000).date);
  const [timeText, setTimeText] = useState(() => fromParam?.time ?? `${dateInputs(Date.now() + 5 * 86_400_000).time.slice(0, 2)}:00`);
  const e = stats.data ? estimator(stats.data) : null;
  const countDown = (h: number) => {
    setHeightText(formatCount(h));
    setMode("height");
  };

  let body: ReactNode;
  if (!stats.data) {
    body = stats.error ? (
      <StateBox
        tone="error"
        icon={<LuTriangleAlert size={20} />}
        title="The estimate could not be loaded"
        actions={
          <Button size="sm" colorPalette="blue" onClick={() => void stats.refetch()}>
            Try again
          </Button>
        }
        data-testid="future-error"
      >
        The explorer did not answer. Try again in a moment.
      </StateBox>
    ) : (
      <Skeleton h="10" w="full" maxW="560px" />
    );
  } else if (!e) {
    body = (
      <Text color="fg.muted" data-testid="future-no-history">
        Not enough history to estimate yet.
      </Text>
    );
  } else if (mode === "height") {
    body = <ByHeight e={e} now={now} text={heightText} onText={setHeightText} />;
  } else {
    body = <ByDate e={e} now={now} date={dateText} time={timeText} onDate={setDateText} onTime={setTimeText} onCountDown={countDown} />;
  }

  return (
    <Box>
      <VTabsBar mt="5" mb="0">
        <VTabs
          id="future"
          label="Estimate by"
          tabs={[
            { value: "height", label: "By block height" },
            { value: "date", label: "By date" },
          ]}
          value={mode}
          onChange={setMode}
        />
      </VTabsBar>
      <VTabPanel id="future" value={mode}>
        <Box mt="4" p={CALC_PAD} bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3">
          {body}
        </Box>
      </VTabPanel>
      {e && (
        <Grid templateColumns={PANELS} gap="4" alignItems="start" mt="5">
          <PlannedBlocks tip={tipAt(e, now)} onCountDown={countDown} />
          <HowItWorks e={e} windows={stats.data?.block_time_windows ?? []} />
        </Grid>
      )}
    </Box>
  );
}

/** `/blocks/future`: when a block will be produced, or which block a date falls on. */
export function FutureBlockPage(props: EstimatorProps) {
  return (
    <Box>
      <Breadcrumb path={[{ label: "Blocks", href: "/blocks" }]} current="Future block" />
      <DetailTitle title="Future block" />
      <Text mt="2" maxW="640px" color="fg.muted" lineHeight="1.55">
        Find when a block will be produced, or which block the chain will be at on a date. An estimate, from the average block time.
      </Text>
      <FutureBlockEstimator {...props} />
    </Box>
  );
}
