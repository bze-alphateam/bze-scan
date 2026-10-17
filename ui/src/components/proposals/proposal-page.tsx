"use client";

import { Box, Button, Flex, Grid, Heading, Link, Skeleton, Stack, Text, type BoxProps } from "@chakra-ui/react";
import NextLink from "next/link";
import { useId, useState, type ReactNode } from "react";
import { LuChevronDown, LuCircleHelp, LuTriangleAlert } from "react-icons/lu";

import { isNotFound } from "@/api/client";
import {
  useAllValidators,
  useDenoms,
  useParams,
  useProposal,
  useProposalDeposits,
  useProposalNonVoters,
  useProposalVotes,
  useStats,
  useStatus,
  useValidatorVotes,
} from "@/api/hooks";
import type { ActiveSet, Proposal, ProposalNonVoter, ProposalVote, TokenSummary, VoteOption } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Chip } from "@/components/ui/chip";
import { DetailList, DetailRow } from "@/components/ui/detail-list";
import { FilterPills } from "@/components/ui/filter-pills";
import { JsonBlock } from "@/components/ui/json-block";
import { ListPanel } from "@/components/ui/list";
import { Markdown } from "@/components/ui/markdown";
import { MoreDetails } from "@/components/ui/more-details";
import { Breadcrumb, PanelHead } from "@/components/ui/page-head";
import { CardSkeleton, ListSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { Tile } from "@/components/ui/tile";
import { TimeAgo } from "@/components/ui/time-ago";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { VTabs, VTabPanel, VTabsBar } from "@/components/ui/vtabs";
import { formatCount, formatDate, formatDateTime, formatPct, shortHash } from "@/lib/format";
import { pingPubProposal, voteDestinations } from "@/lib/links";
import {
  checks,
  coinsText,
  depositsReturned,
  describeProposal,
  endsIn,
  govRules,
  isOpen,
  isTallied,
  kindLabel,
  metadataView,
  notVoted,
  optionText,
  statusInfo,
  tallyShares,
  timeline,
  upgradeEta,
  verdict,
  votesSummary,
  votingLength,
  votingSpan,
  type Action,
  type Checks,
} from "@/render/proposals";

import { ChecksList, HistoricalChip, OPTION_COLORS, ProposalStatusTag, TallyBars, VerdictTag } from "./parts";
import { ActionView } from "./what-it-does";

// Custom breakpoints are not in Chakra's generated types, so responsive objects using them are hoisted.
// Five tiles in a row from c3, three below, two on phones with the last one full width.
const TILES = { base: "repeat(2, minmax(0, 1fr))", c2: "repeat(3, minmax(0, 1fr))", c3: "repeat(4, minmax(0, 1fr)) minmax(0, 1.25fr)" };
const LAST_TILE = { base: "1 / -1", c2: "auto" };
// The page's two columns (1.6fr | 1fr) from 1000px, one below.
const TWO_COLUMNS = { "@media (min-width: 1000px)": { gridTemplateColumns: "minmax(0, 1.6fr) minmax(0, 1fr)" } };
// Validator · Vote · Share of stake (c2) · Time; Account · Vote · Time.
const VALIDATOR_VOTE = { base: "minmax(0, 1fr) auto auto", c2: "minmax(0, 1fr) 100px 110px 100px" };
const VOTE = { base: "minmax(0, 1fr) auto auto", c2: "minmax(0, 1fr) 100px 100px" };

const blockHref = (height: number) => `/blocks/${height}`;

/** A panel with the small upper-case head and a padded body. */
function Section({ id, title, note, children, ...props }: { id: string; title: string; note?: ReactNode; children: ReactNode } & Omit<BoxProps, "title">) {
  return (
    <ListPanel as="section" aria-labelledby={id} {...props}>
      <PanelHead id={id} title={title} note={note} />
      <Box px="4" pt="14px" pb="4" fontSize="13px">
        {children}
      </Box>
    </ListPanel>
  );
}

function Header({ p, now }: { p: Proposal; now: Date }) {
  return (
    <Box pb="5">
      <Breadcrumb path={[{ label: "Proposals", href: "/proposals" }]} current={`#${p.id}`} mono />
      <Flex align="center" gap="10px" wrap="wrap" fontSize="13px" mt="3" mb="2">
        <ProposalStatusTag p={p} now={now} />
        <Text as="span" color="fg.muted">
          {kindLabel(p.kind)}
          {p.expedited ? " · expedited" : ""} ·{" "}
          <Text as="span" fontFamily="mono">
            #{p.id}
          </Text>
        </Text>
      </Flex>
      <Heading as="h1" fontSize="24px" fontWeight="600" letterSpacing="-0.01em" lineHeight="1.25" overflowWrap="anywhere">
        {p.title}
      </Heading>
    </Box>
  );
}

const Mono = ({ children }: { children: ReactNode }) => (
  <Text as="span" fontFamily="mono">
    {children}
  </Text>
);

const join = (parts: ReactNode[]) =>
  parts
    .filter((x) => x != null && x !== false && x !== "")
    .flatMap((x, i) => (i === 0 ? [x] : [" · ", x]))
    .map((x, i) => <span key={i}>{x}</span>);

/**
 * The five tiles under the title. Resolved: Result · Yes · Turnout · Voting ·
 * Proposed by. Voting: If it ended now · Yes so far · Turnout so far · Voting
 * ends · Proposed by. Exact times stay in the Timeline.
 */
function SummaryTiles({ p, c, now, denoms }: { p: Proposal; c: Checks | null; now: Date; denoms?: Map<string, TokenSummary> }) {
  const voting = p.status === "voting_period";
  const yesPct = c?.yes.value ?? tallyShares(p.tally)[0].pct;
  const yesAmount = p.tally ? coinsText([{ denom: "ubze", amount: p.tally.yes }], denoms, true) : null;
  const yes = (
    <Tile
      label={voting ? "Yes so far" : "Yes"}
      value={p.tally && yesPct != null ? formatPct(yesPct) : null}
      sub={join([yesAmount && <Mono>{yesAmount}</Mono>, c && `more than ${formatPct(c.yes.need, { trim: true })} needed`])}
    />
  );
  const turnout = (
    <Tile
      label={voting ? "Turnout so far" : "Turnout"}
      value={p.turnout_pct != null ? formatPct(p.turnout_pct) : null}
      sub={join([
        c && `${formatPct(c.turnout.need, { trim: true })} needed`,
        c && c.turnout.ok !== null && (voting || c.turnout.value == null) && (c.turnout.ok ? "reached" : "not reached yet"),
        p.tally_source === "archive" && <HistoricalChip p={p} />,
      ])}
    />
  );
  const proposer = (
    <Tile
      label="Proposed by"
      small
      gridColumn={LAST_TILE}
      value={p.proposer ? <AccountChip address={p.proposer} label={p.proposer_label} /> : null}
      sub={`Submitted ${formatDateTime(p.submit_time, { seconds: false })}`}
    />
  );

  if (voting) {
    const v = c && verdict(c);
    return (
      <Grid as="section" aria-label="Summary" templateColumns={TILES} gap="3" mb="6" data-testid="summary-tiles">
        <Tile
          label="If it ended now"
          value={v ? (v.pass === null ? "Not known" : v.pass ? "Passes" : "Fails") : null}
          valueColor={v && v.pass !== null ? (v.pass ? "pos.fg" : "neg.fg") : undefined}
          sub={v ? (v.pass ? "all three checks met" : v.text.replace(/^Would fail: /, "")) : undefined}
        />
        {yes}
        {turnout}
        <Tile
          label="Voting ends"
          small
          highlight
          value={p.voting_end_time ? endsIn(p.voting_end_time, now) : null}
          sub={p.voting_end_time && formatDateTime(p.voting_end_time, { seconds: false })}
        />
        {proposer}
      </Grid>
    );
  }

  const status = statusInfo(p, now);
  const resolvedIn =
    p.resolved_height != null ? (
      <>
        {p.status === "passed" ? "executed in block " : "in block "}
        <Link asChild color="accent.fg" fontFamily="mono">
          <NextLink href={blockHref(p.resolved_height)}>{formatCount(p.resolved_height)}</NextLink>
        </Link>
      </>
    ) : null;
  const ended = p.voting_end_time && new Date(p.voting_end_time) <= now;
  return (
    <Grid as="section" aria-label="Summary" templateColumns={TILES} gap="3" mb="6" data-testid="summary-tiles">
      <Tile
        label="Result"
        value={status.text}
        valueColor={status.tone === "green" ? "pos.fg" : status.tone === "red" ? "neg.fg" : undefined}
        sub={join([ended && formatDate(p.voting_end_time!), resolvedIn])}
      />
      {yes}
      {turnout}
      <Tile
        label="Voting"
        small
        value={p.voting_start_time && p.voting_end_time ? votingSpan(p.voting_start_time, p.voting_end_time) : null}
        sub={
          p.voting_start_time && p.voting_end_time
            ? `${votingLength(p.voting_start_time, p.voting_end_time)} · ${ended ? "ended" : status.text.toLowerCase()}`
            : p.status === "deposit_period" && status.text === "Deposit period"
              ? "opens once the deposit is reached"
              : "never opened"
        }
      />
      {proposer}
    </Grid>
  );
}

const SUMMARY_FOLD = 600;

function FromProposer({ p, divided }: { p: Proposal; divided: boolean }) {
  const [open, setOpen] = useState(false);
  const summary = p.summary?.trim() ?? "";
  const long = summary.length > SUMMARY_FOLD;
  const meta = metadataView(p.metadata);
  if (!summary && !meta) return null;
  return (
    <Box mt={divided ? "18px" : "0"} pt={divided ? "14px" : "0"} borderTopWidth={divided ? "1px" : "0"} borderColor="border.panel" data-testid="from-proposer">
      <Heading as="h3" textStyle="tileLabel" fontWeight="600" mb="2">
        From the proposer
      </Heading>
      {summary && (
        // Folded by height, not by cutting the text, so the Markdown stays whole.
        <Box
          position="relative"
          maxH={long && !open ? "150px" : undefined}
          overflow="hidden"
          fontSize="13.5px"
          lineHeight="1.6"
          data-testid="proposer-text"
          data-folded={long && !open ? "" : undefined}
        >
          <Markdown text={summary} />
          {long && !open && (
            <Box position="absolute" insetX="0" bottom="0" h="48px" bgGradient="to-b" gradientFrom="transparent" gradientTo="bg.panel" />
          )}
        </Box>
      )}
      {long && (
        <Button variant="plain" size="sm" px="0" mt="1.5" color="accent.fg" fontWeight="500" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
          {open ? "Show less" : "Read the full text"}
        </Button>
      )}
      {meta &&
        ("href" in meta ? (
          <Link href={meta.href} target="_blank" rel="noopener noreferrer nofollow" display="block" mt="2" color="accent.fg" wordBreak="break-all">
            {meta.href} ↗
          </Link>
        ) : (
          <Text mt="2" color="fg.muted" wordBreak="break-word">
            {meta.text}
          </Text>
        ))}
    </Box>
  );
}

function WhatItDoes({ p, actions, denoms, eta }: { p: Proposal; actions: Action[]; denoms?: Map<string, TokenSummary>; eta: Date | null }) {
  const shown = actions.filter((a) => a.type !== "text");
  return (
    <Section id="what-it-does-title" title={shown.length > 0 ? "What this proposal does" : "What this proposal asks"} data-testid="what-it-does">
      <Stack gap="4">
        {shown.length === 0 && <Text color="fg.muted">A text proposal: it changes nothing on the chain by itself.</Text>}
        {shown.map((a, i) => (
          <ActionView key={i} a={a} p={p} denoms={denoms} upgradeEta={eta} />
        ))}
      </Stack>
      <FromProposer p={p} divided />
    </Section>
  );
}

const MEANINGS: [string, string][] = [
  ["Yes", "you want the change."],
  ["No", "you do not want it."],
  ["Abstain", "you count toward the turnout without taking a side."],
  ["No with veto", "you think it is harmful or spam. Too many vetoes reject it and burn its deposit."],
];

/** What each vote option means, collapsed until asked for. */
function OptionsMeaning() {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <Box mt="2">
      <Button
        variant="plain"
        size="sm"
        px="0"
        gap="1.5"
        color="accent.fg"
        fontWeight="500"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((o) => !o)}
      >
        What the options mean
        <Box as="span" display="inline-flex" transition="transform 0.15s" transform={open ? "rotate(180deg)" : undefined}>
          <LuChevronDown size={12} />
        </Box>
      </Button>
      <Grid as="dl" id={id} hidden={!open} templateColumns="max-content minmax(0, 1fr)" columnGap="3" rowGap="1" m="0" mt="1.5" data-testid="vote-options">
        {MEANINGS.map(([o, m]) => (
          <Box key={o} display="contents">
            <Text as="dt" fontWeight="600">
              {o}
            </Text>
            <Text as="dd" m="0" color="fg.muted">
              {m}
            </Text>
          </Box>
        ))}
      </Grid>
    </Box>
  );
}

function Votes({ p, c, denoms, historical }: { p: Proposal; c: Checks | null; denoms?: Map<string, TokenSummary>; historical: boolean }) {
  const voting = p.status === "voting_period";
  if (p.status === "deposit_period" || p.status === "canceled" || p.status === "dropped") {
    return (
      <Section id="votes-title" title="Votes" data-testid="votes-card">
        <Text color="fg.muted">
          {p.status === "canceled"
            ? "The proposer withdrew this proposal before voting ended."
            : p.status === "dropped"
              ? "The deposit was not reached in time, so voting never opened."
              : "Voting opens once the deposit is reached."}
        </Text>
      </Section>
    );
  }
  return (
    <Section id="votes-title" title={voting ? "Votes so far" : "Final result"} note={voting ? "refreshed every minute" : undefined} data-testid="votes-card">
      {p.tally ? <TallyBars tally={p.tally} denoms={denoms} amounts /> : <Text color="fg.muted">Not tallied yet.</Text>}
      {voting && c && (
        <Flex align="center" gap="2" wrap="wrap" mt="3">
          <VerdictTag v={verdict(c)} />
          <Text as="span" color="fg.muted">
            if voting ended now
          </Text>
        </Flex>
      )}
      {c ? (
        <ChecksList c={c} p={p} />
      ) : (
        p.tally && <Text mt="3" color="fg.muted">The voting rules could not be loaded, so the checks are not shown.</Text>
      )}
      <Text mt="2" color="fg.muted" lineHeight="1.55">
        A validator&apos;s vote counts for everyone staking with it, unless the staker votes themselves.{" "}
        <Mono>{formatCount(p.votes_count)}</Mono> {p.votes_count === 1 ? "account has" : "accounts have"} voted{voting ? " so far" : ""}.
      </Text>
      <OptionsMeaning />
      {historical && (
        <Text mt="2" color="fg.muted" lineHeight="1.55" data-testid="historical-explainer">
          Some votes were read from the chain&apos;s state at the end of voting (marked <Chip variant="hist">historical</Chip>). The chain
          deletes votes once a proposal is tallied, so these show the final choice but not when it was made.
        </Text>
      )}
    </Section>
  );
}

function WantToVote({ p }: { p: Proposal }) {
  return (
    <Section id="want-to-vote-title" title="Want to vote?" data-testid="want-to-vote">
      <Text lineHeight="1.55">
        The explorer only shows the vote. Vote from a wallet with the account you stake from: your vote then counts for your stake
        instead of your validator&apos;s, and you can change it until voting ends.
      </Text>
      <Stack gap="2" mt="3" role="list" aria-label="Where to vote">
        {voteDestinations(p.id).map((d) => (
          <Box key={d.href} role="listitem">
            <Button asChild colorPalette="blue" size="sm" h="36px">
              <a href={d.href} target="_blank" rel="noopener noreferrer">
                {d.label} ↗
              </a>
            </Button>
          </Box>
        ))}
      </Stack>
      <Text mt="2.5" color="fg.muted">
        More places to vote will be listed here as they open.
      </Text>
    </Section>
  );
}

const HISTORICAL_VOTE = "Vote as recorded on chain when voting ended; the voting transaction itself is not indexed.";

/** When a vote was cast: its transaction's age, or the historical chip linking to the tally block. */
function VoteTime({ v }: { v: ProposalVote }) {
  if (v.source === "archive") {
    // The time is the tally block's, not the vote's: never shown.
    return (
      <Link asChild _hover={{ textDecoration: "none" }}>
        <NextLink href={blockHref(v.height)} title={HISTORICAL_VOTE} data-testid="historical-vote">
          <Chip variant="hist">historical</Chip>
        </NextLink>
      </Link>
    );
  }
  if (v.tx_hash) {
    return (
      <Link asChild color="fg.muted">
        <NextLink href={`/txs/${v.tx_hash.toUpperCase()}`}>
          <TimeAgo time={v.time} />
        </NextLink>
      </Link>
    );
  }
  return <TimeAgo time={v.time} />;
}

const optionColor = (o: VoteOption | null) => (o ? OPTION_COLORS[o] : "fg.muted");

function VoteHead({ validator }: { validator?: boolean }) {
  return (
    <Grid templateColumns={validator ? VALIDATOR_VOTE : VOTE} gap="3" py="2" borderBottomWidth="1px" borderColor="border.panel" textStyle="tableHead" aria-hidden>
      <Text>{validator ? "Validator" : "Account"}</Text>
      <Text>Vote</Text>
      {validator && (
        <Text hideBelow="c2" textAlign="end">
          Share of stake
        </Text>
      )}
      <Text textAlign="end">Time</Text>
    </Grid>
  );
}

function VoteRow({ v, validator }: { v: ProposalVote; validator?: boolean }) {
  return (
    <Grid
      role="listitem"
      templateColumns={validator ? VALIDATOR_VOTE : VOTE}
      gap="3"
      alignItems="center"
      py="9px"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      data-testid={validator ? "validator-vote" : "vote-row"}
    >
      <Box minW="0">
        {validator && v.validator_operator ? (
          <ValidatorChip operator={v.validator_operator} moniker={v.validator} />
        ) : (
          <AccountChip address={v.voter} label={v.voter_label ?? (v.validator ? { name: v.validator, kind: "validator_owner" } : null)} />
        )}
      </Box>
      <Text fontWeight="600" color={optionColor(v.option)}>
        {optionText(v.option)}
      </Text>
      {validator && (
        <Text hideBelow="c2" textAlign="end" fontFamily="mono" color="fg.muted">
          {v.voting_power_pct != null ? formatPct(v.voting_power_pct) : "—"}
        </Text>
      )}
      <Box textAlign="end" fontSize="12px" color="fg.muted" whiteSpace="nowrap">
        <VoteTime v={v} />
      </Box>
    </Grid>
  );
}

const NOT_VOTED_SHOWN = 5;

function ValidatorsTab({ p, votes }: { p: Proposal; votes: ProposalVote[] | undefined }) {
  if (!votes) return <ListSkeleton rows={5} />;
  const voting = p.status === "voting_period";
  return (
    <Box>
      {votes.length === 0 ? (
        <Text color="fg.muted" py="2">
          No validator has voted{voting ? " yet" : ""}.
        </Text>
      ) : (
        <>
          <VoteHead validator />
          <Box role="list" aria-label="Validator votes">
            {votes.map((v) => (
              <VoteRow key={v.voter} v={v} validator />
            ))}
          </Box>
        </>
      )}
      {p.active_set && !voting ? (
        <RecordedNotVoted id={p.id} set={p.active_set} />
      ) : (
        <TodayNotVoted votes={votes} voting={voting} recording={isTallied(p.status)} />
      )}
    </Box>
  );
}

/** The line under the validator votes naming the ones that did not vote. */
function NotVotedLine({ children }: { children: ReactNode }) {
  return (
    <Flex align="center" gap="6px 10px" wrap="wrap" mt="3" pt="3" borderTopWidth="1px" borderColor="border.panel" data-testid="not-voted">
      {children}
    </Flex>
  );
}

/**
 * Who did not vote, measured against today's active set: the voters while
 * voting is open; once it closed, said as today's set (it may differ from the
 * set at the time) until the explorer has recorded the set the tally counted.
 */
function TodayNotVoted({ votes, voting, recording }: { votes: ProposalVote[]; voting: boolean; recording: boolean }) {
  const validators = useAllValidators();
  const [all, setAll] = useState(false);
  const missing = validators.data ? notVoted(validators.data.items, votes) : [];
  const shown = all ? missing : missing.slice(0, NOT_VOTED_SHOWN);
  if (missing.length === 0) return null;
  return (
    <NotVotedLine>
      <Text as="span" color="fg.muted">
        {voting ? (
          <>
            <Mono>{missing.length}</Mono> {missing.length === 1 ? "validator has" : "validators have"} not voted yet:
          </>
        ) : (
          <>
            <Mono>{missing.length}</Mono> of today&apos;s active validators did not vote:
          </>
        )}
      </Text>
      {shown.map((v) => (
        <ValidatorChip key={v.operator_address} operator={v.operator_address} moniker={v.moniker} />
      ))}
      {missing.length > shown.length && (
        <Button variant="plain" size="xs" px="0" color="accent.fg" onClick={() => setAll(true)}>
          Show all
        </Button>
      )}
      {voting && (
        <Text as="span" color="fg.muted">
          · their stakers can still vote for themselves
        </Text>
      )}
      {recording && (
        <Text color="fg.muted" fontSize="13px" w="full" data-testid="non-voters-pending">
          The exact list is being recorded.
        </Text>
      )}
    </NotVotedLine>
  );
}

/** Who did not vote, as recorded: the validators of the set the tally counted. */
function RecordedNotVoted({ id, set }: { id: number; set: ActiveSet }) {
  const nonVoters = useProposalNonVoters(id, set.non_voters_count > 0);
  const [all, setAll] = useState(false);
  if (set.non_voters_count === 0) {
    return (
      <NotVotedLine>
        <Text as="span" color="fg.muted">
          All <Mono>{set.size}</Mono> active validators voted.
        </Text>
      </NotVotedLine>
    );
  }
  const items = nonVoters.data?.pages.flatMap((page) => page.items);
  const shown = items && (all ? items : items.slice(0, NOT_VOTED_SHOWN));
  return (
    <NotVotedLine>
      <Text as="span" color="fg.muted">
        <Mono>{set.non_voters_count}</Mono> of the <Mono>{set.size}</Mono> active validators did not vote:
      </Text>
      {!shown ? (
        nonVoters.error ? (
          <Text as="span" color="fg.muted">
            The list could not be loaded.
          </Text>
        ) : (
          <Skeleton as="span" h="4" w="48" maxW="full" display="inline-block" />
        )
      ) : (
        shown.map((n) => <NonVoterChip key={n.consensus_address} n={n} />)
      )}
      {items && !all && items.length > NOT_VOTED_SHOWN && (
        <Button variant="plain" size="xs" px="0" color="accent.fg" onClick={() => setAll(true)}>
          Show all
        </Button>
      )}
      {all && nonVoters.hasNextPage && (
        <Button variant="plain" size="xs" px="0" color="accent.fg" disabled={nonVoters.isFetchingNextPage} onClick={() => void nonVoters.fetchNextPage()}>
          Show more
        </Button>
      )}
    </NotVotedLine>
  );
}

/** A recorded non-voter: the validator's chip, or its short consensus address when no validator is known with it; its share of the set's power. */
function NonVoterChip({ n }: { n: ProposalNonVoter }) {
  return (
    <Flex as="span" align="center" gap="1.5" minW="0" maxW="full" data-testid="non-voter">
      {n.operator_address ? (
        <ValidatorChip operator={n.operator_address} moniker={n.moniker} />
      ) : (
        <Text as="span" fontFamily="mono" title={n.consensus_address}>
          {shortHash(n.consensus_address)}
        </Text>
      )}
      <Text as="span" color="fg.muted" fontSize="12px" fontFamily="mono">
        {formatPct(n.power_pct)}
      </Text>
    </Flex>
  );
}

type VoteFilter = VoteOption | "weighted" | null;

const VOTE_FILTERS: { label: string; value: VoteFilter }[] = [
  { label: "All", value: null },
  { label: "Yes", value: "yes" },
  { label: "No", value: "no" },
  { label: "Abstain", value: "abstain" },
  { label: "Veto", value: "no_with_veto" },
  { label: "Split", value: "weighted" },
];

function EveryoneTab({ p }: { p: Proposal }) {
  const [option, setOption] = useState<VoteFilter>(null);
  const votes = useProposalVotes(p.id, option, isOpen(p.status));
  const items = votes.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <Box>
      <FilterPills label="Show votes" options={VOTE_FILTERS} value={option} onChange={setOption} mb="1.5" />
      {!votes.data ? (
        votes.error ? (
          <Text color="fg.muted" py="2">
            The votes could not be loaded.
          </Text>
        ) : (
          <ListSkeleton rows={5} />
        )
      ) : items.length === 0 ? (
        <Text color="fg.muted" py="2">
          No votes here.
        </Text>
      ) : (
        <>
          <VoteHead />
          <Box role="list" aria-label="Votes">
            {items.map((v) => (
              <VoteRow key={v.voter} v={v} />
            ))}
          </Box>
        </>
      )}
      {votes.isFetchingNextPage && <ListSkeleton rows={5} />}
      {votes.hasNextPage && !votes.isFetchingNextPage && (
        <Button variant="outline" size="sm" mt="2.5" onClick={() => void votes.fetchNextPage()}>
          Show more votes
        </Button>
      )}
    </Box>
  );
}

type VoteTab = "validators" | "everyone";

function WhoVoted({ p }: { p: Proposal }) {
  const open = isOpen(p.status);
  const validatorVotes = useValidatorVotes(p.id, open);
  const validators = useAllValidators();
  const [expanded, setExpanded] = useState(open);
  const [tab, setTab] = useState<VoteTab>("validators");
  // Who did not vote is measured against today's active set: only right while voting is open.
  const missing = open ? (validatorVotes.data && validators.data ? notVoted(validators.data.items, validatorVotes.data).length : null) : 0;
  const tabs = [
    {
      value: "validators" as const,
      label: "Validators",
      count: p.validators_voted.total != null ? `${p.validators_voted.voted} of ${p.validators_voted.total}` : p.validators_voted.voted,
    },
    { value: "everyone" as const, label: "Everyone", count: formatCount(p.votes_count) },
  ];
  return (
    <Section id="who-voted-title" title="Who voted" data-testid="who-voted">
      {!open && (
        <Text fontSize="14px" mb="3" data-testid="votes-summary">
          {validatorVotes.data && missing != null ? (
            votesSummary(validatorVotes.data, missing, p.votes_count)
          ) : (
            <Skeleton as="span" h="4" w="64" maxW="full" display="inline-block" />
          )}
        </Text>
      )}
      {!expanded ? (
        <Button variant="outline" size="sm" onClick={() => setExpanded(true)}>
          See every vote
        </Button>
      ) : (
        <>
          <VTabsBar mb="1.5">
            <VTabs id="votes" label="Votes" tabs={tabs} value={tab} onChange={setTab} />
          </VTabsBar>
          <VTabPanel id="votes" value={tab}>
            {tab === "validators" ? <ValidatorsTab p={p} votes={validatorVotes.data} /> : <EveryoneTab p={p} />}
          </VTabPanel>
        </>
      )}
    </Section>
  );
}

function Timeline({ items }: { items: ReturnType<typeof timeline> }) {
  return (
    <ListPanel as="section" aria-labelledby="timeline-title">
      <PanelHead id="timeline-title" title="Timeline" />
      <Box as="ol" listStyleType="none" m="0" px="4" pt="3" pb="14px" aria-label="Timeline">
        {items.map((item, i) => (
          <Flex
            as="li"
            key={item.label}
            gap="3"
            align="flex-start"
            position="relative"
            pt="6px"
            pb="10px"
            fontSize="13px"
            // The line joining a step to the next one.
            _before={
              i < items.length - 1
                ? { content: '""', position: "absolute", left: "4px", top: "18px", bottom: "-4px", w: "2px", bg: "border.panel" }
                : undefined
            }
            data-testid="timeline-item"
            data-done={item.done}
          >
            <Box
              flex="none"
              boxSize="10px"
              mt="4px"
              rounded="full"
              borderWidth="2px"
              borderColor="accent.fg"
              bg={item.done ? "accent.fg" : "bg.panel"}
              position="relative"
              zIndex="1"
              aria-hidden
            />
            <Box minW="0">
              <Text fontWeight="500" color={item.done ? "fg" : "fg.muted"}>
                {item.label}
              </Text>
              <Text mt="1px" fontFamily="mono" fontSize="12px" color="fg.muted">
                {item.time && `${item.done ? "" : "about "}${formatDateTime(item.time)}`}
                {item.height != null && (
                  <>
                    {item.time ? " · " : ""}block{" "}
                    <Link asChild color="fg.muted">
                      <NextLink href={blockHref(item.height)}>{formatCount(item.height)}</NextLink>
                    </Link>
                  </>
                )}
              </Text>
            </Box>
          </Flex>
        ))}
      </Box>
    </ListPanel>
  );
}

function Details({ p, actions, vetoed, denoms }: { p: Proposal; actions: Action[]; vetoed: boolean; denoms?: Map<string, TokenSummary> }) {
  const deposits = useProposalDeposits(p.id);
  const returned = depositsReturned(p.status, vetoed);
  const upgrade = actions.find((a) => a.type === "upgrade");
  return (
    <Stack gap="3">
      <ListPanel fontSize="13px">
        <DetailList>
          <DetailRow label="Message types">
            <Stack gap="0.5">
              {p.message_types.length === 0
                ? "none"
                : p.message_types.map((t, i) => (
                    <Text key={`${t}-${i}`} fontFamily="mono" fontSize="12px" wordBreak="break-all">
                      {t}
                    </Text>
                  ))}
            </Stack>
          </DetailRow>
          {upgrade && (
            <DetailRow label="Upgrade">
              <Text as="span">
                {upgrade.name}
                {upgrade.height != null && (
                  <>
                    {" "}
                    at block{" "}
                    <Link asChild color="accent.fg" fontFamily="mono">
                      <NextLink href={blockHref(upgrade.height)}>{formatCount(upgrade.height)}</NextLink>
                    </Link>
                  </>
                )}
              </Text>
            </DetailRow>
          )}
          <DetailRow label="Deposits">
            {!deposits.data ? (
              deposits.error ? (
                "could not be loaded"
              ) : (
                <Skeleton h="4" w="48" />
              )
            ) : deposits.data.length === 0 ? (
              "none indexed"
            ) : (
              <Stack gap="1" data-testid="deposits">
                {deposits.data.map((d, i) => (
                  <Flex key={`${d.depositor}-${d.height}-${i}`} gap="1.5" align="center" wrap="wrap">
                    <Text as="span">
                      <Mono>{coinsText(d.amount, denoms)}</Mono>
                      {d.source === "archive" && " in total · historical ·"} by
                    </Text>
                    <AccountChip address={d.depositor} label={d.depositor_label} />
                    {returned && (
                      <Text as="span" color="fg.muted">
                        · returned
                      </Text>
                    )}
                  </Flex>
                ))}
              </Stack>
            )}
          </DetailRow>
          <DetailRow label="Tally source" last>
            <Text as="span" color="fg.muted">
              {isOpen(p.status) ? "Live from the chain, refreshed every minute" : "The chain's final tally"}
              {p.tally_updated_at && ` · as of ${formatDateTime(p.tally_updated_at)}`}
            </Text>
          </DetailRow>
        </DetailList>
      </ListPanel>
      <JsonBlock value={p} />
      <Link href={pingPubProposal(p.id)} target="_blank" rel="noopener noreferrer" fontSize="13px" color="accent.fg" alignSelf="start">
        Open on Ping.pub ↗
      </Link>
    </Stack>
  );
}

/**
 * Whether a vote or deposit of the proposal is one the snapshots command read
 * from the archive. The queries are the ones the vote tabs and the deposits
 * list run, so they are shared, not repeated.
 */
function useHasHistoricalRows(p: Proposal): boolean {
  const open = isOpen(p.status);
  const validatorVotes = useValidatorVotes(p.id, open);
  const votes = useProposalVotes(p.id, null, open);
  const deposits = useProposalDeposits(p.id);
  const rows: { source: string }[] = [
    ...(validatorVotes.data ?? []),
    ...(votes.data?.pages.flatMap((page) => page.items) ?? []),
    ...(deposits.data ?? []),
  ];
  return rows.some((r) => r.source === "archive");
}

/** A found proposal. */
export function ProposalView({ p }: { p: Proposal }) {
  const now = new Date();
  const params = useParams();
  const denoms = useDenoms();
  const stats = useStats();
  const status = useStatus();
  const actions = describeProposal(p);
  const c = checks(p, govRules(params.data, p.expedited));
  const upgrade = actions.find((a) => a.type === "upgrade");
  const dbHeight = status.data?.live_fill.db_height ?? stats.data?.latest_height ?? null;
  const eta = upgrade?.height != null ? upgradeEta(upgrade.height, dbHeight, stats.data?.avg_block_time_ms, now) : null;
  const reached = upgrade?.height != null && dbHeight != null && dbHeight >= upgrade.height;
  const depositText = p.total_deposit && p.total_deposit.length > 0 ? coinsText(p.total_deposit, denoms.data) : null;
  const items = timeline(p, depositText, upgrade?.height != null ? { height: upgrade.height, eta, reached } : null, now);
  const historical = useHasHistoricalRows(p);
  return (
    <Box>
      <Header p={p} now={now} />
      <SummaryTiles p={p} c={c} now={now} denoms={denoms.data} />
      <Grid templateColumns="minmax(0, 1fr)" css={TWO_COLUMNS} gap="5" alignItems="start">
        <Stack gap="5" minW="0">
          <WhatItDoes p={p} actions={actions} denoms={denoms.data} eta={eta} />
          <WhoVoted p={p} />
        </Stack>
        <Stack gap="5" minW="0">
          <Votes p={p} c={c} denoms={denoms.data} historical={historical} />
          {p.status === "voting_period" && <WantToVote p={p} />}
          <Timeline items={items} />
        </Stack>
      </Grid>
      <Box mt="4">
        <MoreDetails hint="message types, deposits, raw proposal">
          <Details p={p} actions={actions} vetoed={c != null && !c.veto.ok} denoms={denoms.data} />
        </MoreDetails>
      </Box>
    </Box>
  );
}

export function ProposalSkeleton() {
  return (
    <Box aria-busy data-testid="proposal-skeleton">
      <Stack gap="3" pt="6" pb="5">
        <Skeleton h="3" w="28" />
        <Skeleton h="4" w="48" />
        <Skeleton h="7" w="96" maxW="full" />
      </Stack>
      <Grid templateColumns={TILES} gap="3" mb="6">
        {Array.from({ length: 5 }).map((_, i) => (
          <Box key={i} gridColumn={i === 4 ? LAST_TILE : undefined}>
            <TileSkeleton />
          </Box>
        ))}
      </Grid>
      <Grid templateColumns="minmax(0, 1fr)" css={TWO_COLUMNS} gap="5">
        <Stack gap="5">
          <CardSkeleton lines={5} />
          <ListSkeleton rows={5} />
        </Stack>
        <Stack gap="2" p="5" bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" aria-hidden>
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} h="1.5" w="full" data-testid="tally-skeleton" />
          ))}
        </Stack>
      </Grid>
    </Box>
  );
}

/** The breadcrumb and title of a proposal that did not load. */
function MissingHead({ id }: { id: number }) {
  return (
    <Box pb="6">
      <Breadcrumb path={[{ label: "Proposals", href: "/proposals" }]} current={`#${id}`} mono />
      <Heading as="h1" fontSize="24px" fontWeight="600" letterSpacing="-0.01em" lineHeight="1.25" mt="2">
        Proposal{" "}
        <Text as="span" fontFamily="mono">
          #{id}
        </Text>
      </Heading>
    </Box>
  );
}

/** `/proposals/[id]`: the skeleton, then the proposal (polled while open), not found on a 404, or the error state. */
export function ProposalPage({ id }: { id: number }) {
  const { data, error, refetch } = useProposal(id);
  if (data) return <ProposalView p={data} />;
  if (isNotFound(error)) {
    return (
      <Box>
        <MissingHead id={id} />
        <StateBox icon={<LuCircleHelp size={20} />} title="Proposal not found" data-testid="proposal-not-found">
          The explorer does not know proposal #{id}. Check the link, or find it in the{" "}
          <Link asChild color="accent.fg">
            <NextLink href="/proposals">proposals list</NextLink>
          </Link>
          .
        </StateBox>
      </Box>
    );
  }
  if (error) {
    return (
      <Box>
        <MissingHead id={id} />
        <StateBox
          tone="error"
          icon={<LuTriangleAlert size={20} />}
          title="This proposal could not be loaded"
          actions={
            <Button size="sm" colorPalette="blue" onClick={() => void refetch()}>
              Try again
            </Button>
          }
          data-testid="proposal-error"
        >
          The explorer did not answer. Try again in a moment.
        </StateBox>
      </Box>
    );
  }
  return <ProposalSkeleton />;
}
