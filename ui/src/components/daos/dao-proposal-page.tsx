"use client";

import { Box, Button, Flex, Grid, Heading, Link, Skeleton, Stack, Text } from "@chakra-ui/react";
import BigNumber from "bignumber.js";
import NextLink from "next/link";
import { useId, useState, type ReactNode } from "react";
import { LuCircleHelp, LuTriangleAlert } from "react-icons/lu";

import { isNotFound } from "@/api/client";
import { useDaoProposal, useDaoProposalVotes, useDenoms } from "@/api/hooks";
import type { DaoProposal, DaoVote, DaoVoteOption, TokenSummary } from "@/api/types";
import { CheckRow } from "@/components/proposals/parts";
import { AccountChip } from "@/components/ui/account-chip";
import { Chip } from "@/components/ui/chip";
import { FilterPills } from "@/components/ui/filter-pills";
import { JsonBlock } from "@/components/ui/json-block";
import { ListPanel } from "@/components/ui/list";
import { MoreDetails } from "@/components/ui/more-details";
import { Breadcrumb, PanelHead } from "@/components/ui/page-head";
import { CardSkeleton, ListSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { Tile } from "@/components/ui/tile";
import { TimeAgo } from "@/components/ui/time-ago";
import { VTabPanel, VTabs, VTabsBar } from "@/components/ui/vtabs";
import { formatCount, formatDate, formatDateTime, formatPct, shortHash } from "@/lib/format";
import { destinationText, daoHref } from "@/render/daos";
import {
  checkGroups,
  hasMessages,
  isReduced,
  msgType,
  REDUCED_NOTE_MEMBER,
  REDUCED_NOTE_STAKE,
  settleNote,
  shares,
  summaryText,
  weightText,
} from "@/render/dao-proposals";
import { createRenderContext, rendererFor } from "@/render/messages/registry";
import { parseCoins } from "@/render/messages/common";
import { coinsText, endsIn } from "@/render/proposals";

import { DaoChip, WhoChip } from "./parts";
import { DaoStatusTag, DaoTallyBars } from "./proposal-parts";

// Custom breakpoints are not in Chakra's generated types, so responsive objects using them are hoisted.
// Five tiles in a row from c3, three below, two on phones.
const TILES = { base: "repeat(2, minmax(0, 1fr))", c2: "repeat(3, minmax(0, 1fr))", c3: "repeat(5, minmax(0, 1fr))" };
const VOTE_COLUMNS = { base: "minmax(0, 1fr) auto", c2: "minmax(0, 1fr) 90px minmax(0, 200px) 120px" };

const OPTION_FILTERS: { label: string; value: DaoVoteOption | null }[] = [
  { label: "All", value: null },
  { label: "Yes", value: "yes" },
  { label: "No", value: "no" },
  { label: "Abstain", value: "abstain" },
];

const OPTION_COLOR: Record<string, string> = { yes: "pos.fg", no: "neg.fg", abstain: "fg.muted" };
const OPTION_TEXT: Record<string, string> = { yes: "Yes", no: "No", abstain: "Abstain" };

type VoteTab = "overview" | "votes";

const parseTab = (raw: string | undefined): VoteTab => (raw === "votes" ? "votes" : "overview");

/** Keeps the tab in the URL without a navigation; the first tab leaves it out. */
function writeTab(tab: VoteTab) {
  const url = new URL(window.location.href);
  url.search = tab === "overview" ? "" : `?tab=${tab}`;
  window.history.replaceState(window.history.state, "", url);
}

/** A panel with the small upper-case head and a padded body. */
function Section({ id, title, note, children, ...props }: { id: string; title: string; note?: ReactNode; children: ReactNode; "data-testid"?: string }) {
  return (
    <ListPanel as="section" aria-labelledby={id} {...props}>
      <PanelHead id={id} title={title} note={note} />
      <Box px="4" pt="14px" pb="4" fontSize="13px">
        {children}
      </Box>
    </ListPanel>
  );
}

const Mono = ({ children }: { children: ReactNode }) => (
  <Text as="span" fontFamily="mono">
    {children}
  </Text>
);

const txLink = (hash: string) => (
  <Link asChild color="accent.fg" fontFamily="mono">
    <NextLink href={`/txs/${hash.toUpperCase()}`} title={hash} data-testid="settle-tx">
      {shortHash(hash)}
    </NextLink>
  </Link>
);

const blockLink = (height: number) => (
  <Link asChild color="accent.fg" fontFamily="mono">
    <NextLink href={`/blocks/${height}`}>{formatCount(height)}</NextLink>
  </Link>
);

function Header({ p }: { p: DaoProposal }) {
  const daoName = p.dao?.name;
  return (
    <Box pb="5">
      <Breadcrumb
        path={[
          { label: "DAOs", href: "/daos" },
          ...(p.dao ? [{ label: daoName ? `DAO #${p.dao.id} · ${daoName}` : `DAO #${p.dao.id}`, href: daoHref(p.dao.id) }] : []),
        ]}
        current={`Proposal #${p.id}`}
      />
      <Flex align="center" gap="10px" wrap="wrap" fontSize="13px" mt="3" mb="2">
        <DaoStatusTag status={p.status} />
        {p.expedited && (
          <Chip variant="acc" data-testid="expedited-tag" title="Can be settled early once the early-settle bars are met">
            Expedited
          </Chip>
        )}
        <Text as="span" color="fg.muted" fontFamily="mono" data-testid="dao-proposal-id">
          #{p.id}
        </Text>
        {p.dao && <DaoChip id={p.dao.id} name={p.dao.name} />}
      </Flex>
      <Heading as="h1" fontSize="24px" fontWeight="600" letterSpacing="-0.01em" lineHeight="1.25" overflowWrap="anywhere">
        {p.title || `Proposal #${p.id}`}
      </Heading>
      {(p.proposer || p.submit_time) && (
        <Flex gap="1.5" align="center" wrap="wrap" mt="2" fontSize="13px" color="fg.muted" data-testid="dao-proposal-meta">
          {p.proposer && (
            <>
              <Text as="span">Proposed by</Text>
              <WhoChip address={p.proposer} label={p.proposer_label} />
            </>
          )}
          {p.submit_time && <Text as="span">{p.proposer ? "·" : ""} submitted {formatDateTime(p.submit_time, { seconds: false })}</Text>}
        </Flex>
      )}
    </Box>
  );
}

/** Yes, No and Abstain with their share of the pinned voting power, turnout against the quorum, and the time. */
function SummaryTiles({ p, now }: { p: DaoProposal; now: Date }) {
  const s = shares(p);
  const voting = p.status === "voting";
  const quorum = typeof p.pinned_config?.min_quorum === "string" ? p.pinned_config.min_quorum : null;
  const quorumPct = quorum && new BigNumber(quorum).isFinite() ? new BigNumber(quorum).times(100).toString() : null;
  const tile = (key: "yes" | "no" | "abstain", label: string, color?: string) => (
    <Tile
      label={label}
      value={s[key] != null ? formatPct(s[key]!) : null}
      valueColor={color}
      sub={p[key] != null ? weightText(p[key], p.weight_denom, true) : null}
      data-testid={`tile-${key}`}
    />
  );
  const turnoutOk = p.checks?.regular.quorum_met;
  let time: ReactNode;
  if (voting) {
    const left = p.voting_end ? endsIn(p.voting_end, now) : null;
    time = (
      <Tile
        label="Voting ends"
        small
        highlight
        value={p.voting_ended || left === "ended" ? "Ended" : left}
        sub={p.voting_end ? formatDateTime(p.voting_end, { seconds: false }) : null}
        data-testid="tile-time"
      />
    );
  } else if (p.status === "cancelled") {
    time = <Tile label="Cancelled" small value={p.cancelled_height != null ? <>block {blockLink(p.cancelled_height)}</> : null} data-testid="tile-time" />;
  } else {
    const settled = p.tally_updated_at ?? p.voting_end;
    time = (
      <Tile
        label={p.tally_updated_at ? "Settled" : "Voting ended"}
        small
        value={settled ? formatDate(settled) : null}
        sub={settled ? formatDateTime(settled, { seconds: false }) : null}
        data-testid="tile-time"
      />
    );
  }
  return (
    <Grid as="section" aria-label="Summary" templateColumns={TILES} gap="3" mb="6" data-testid="dao-proposal-tiles">
      {tile("yes", voting ? "Yes so far" : "Yes", "pos.fg")}
      {tile("no", voting ? "No so far" : "No", "neg.fg")}
      {tile("abstain", "Abstain")}
      <Tile
        label={voting ? "Turnout so far" : "Turnout"}
        value={s.turnout != null ? formatPct(s.turnout) : null}
        sub={[quorumPct != null && `${formatPct(quorumPct, { trim: true })} needed`, turnoutOk != null && (turnoutOk ? "reached" : voting ? "not reached yet" : "not reached")]
          .filter(Boolean)
          .join(" · ")}
        data-testid="tile-turnout"
      />
      {time}
    </Grid>
  );
}

/** The result as checks, the way the governance page shows them; what to do next when voting is over. */
function Result({ p }: { p: DaoProposal }) {
  const groups = checkGroups(p);
  const note = settleNote(p);
  return (
    <Section id="dao-result-title" title="Result" data-testid="dao-result">
      {note && (
        <Chip variant={p.voting_ended ? "warn" : "ok"} data-testid="settle-note" mb="3">
          {note}
        </Chip>
      )}
      {groups.length === 0 ? (
        <Text color="fg.muted">{p.yes == null ? "Not tallied yet." : "The voting rules could not be loaded, so the checks are not shown."}</Text>
      ) : (
        <Stack gap="4">
          {groups.map((g) => (
            <Box key={g.key} data-testid={`checks-${g.key}`}>
              <Text fontWeight="600">{g.title}</Text>
              <Text color="fg.muted" fontSize="12px" mt="1px">
                {g.note}
              </Text>
              <Stack as="ul" listStyleType="none" m="0" mt="3" gap="10px" aria-label={g.title}>
                {g.lines.map((l) => (
                  <CheckRow key={l.key} ok={l.ok} title={l.title} detail={l.detail} />
                ))}
              </Stack>
            </Box>
          ))}
        </Stack>
      )}
      <Box mt="4">
        <DaoTallyBars p={p} weight={p.weight_denom} amounts />
      </Box>
    </Section>
  );
}

/** The proposal's messages, each in plain words through the renderers. */
function WhatItDoes({ p, denoms }: { p: DaoProposal; denoms?: Map<string, TokenSummary> }) {
  const ctx = createRenderContext({ transfers: [], tokens: denoms });
  return (
    <Section id="dao-what-title" title="What this proposal does" data-testid="dao-what-it-does">
      {hasMessages(p) ? (
        <Stack gap="4">
          <Text color="fg.muted" data-testid="runs-as-dao">
            Runs as the DAO&apos;s treasury account.
          </Text>
          {p.msgs!.map((m, i) => {
            const type = msgType(m);
            const r = rendererFor(type);
            const msg = { type_url: type, sender: null, module: null, body: m, events: null, msg_index: null };
            return (
              <Stack key={i} gap="2" data-testid="dao-action" data-type={type}>
                <Text fontWeight="semibold">{r.title(msg, ctx)}</Text>
                <r.Body msg={msg} ctx={ctx} />
              </Stack>
            );
          })}
        </Stack>
      ) : p.message_types.length > 0 ? (
        <Stack gap="2" data-testid="dao-actions-unread">
          <Text color="fg.muted">The messages were not recorded; their types are:</Text>
          {p.message_types.map((t, i) => (
            <Text key={`${t}-${i}`} fontFamily="mono" fontSize="12px" wordBreak="break-all">
              {t}
            </Text>
          ))}
        </Stack>
      ) : (
        <Text color="fg.muted">A text proposal: it changes nothing on the chain by itself.</Text>
      )}
      <ProposerSummary summary={p.summary} />
    </Section>
  );
}

/** What the proposer wrote: plain text with line breaks, collapsed until asked for, never HTML. */
function ProposerSummary({ summary }: { summary: string | null }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const s = summaryText(summary);
  if (!s) return null;
  return (
    <Box mt="18px" pt="14px" borderTopWidth="1px" borderColor="border.panel" data-testid="dao-summary">
      <Button variant="plain" size="sm" px="0" color="accent.fg" fontWeight="500" aria-expanded={open} aria-controls={id} onClick={() => setOpen((o) => !o)}>
        {open ? "Hide the proposer's text" : "From the proposer"}
      </Button>
      <Box id={id} hidden={!open} mt="2">
        <Text whiteSpace="pre-wrap" overflowWrap="anywhere" lineHeight="1.6" fontSize="13.5px" data-testid="dao-summary-text">
          {s.text}
        </Text>
        {s.cut && (
          <Text mt="2" color="fg.muted" fontSize="12px">
            The text is cut after 5,000 characters.
          </Text>
        )}
      </Box>
    </Box>
  );
}

function Destination({ value }: { value: string }) {
  return /^bze1[0-9a-z]{38,}$/.test(value) ? <AccountChip address={value} /> : <Text as="span">{destinationText(value)}</Text>;
}

/** The deposit: refunded to the proposer, or kept and sent where the DAO's rules say. */
function Deposit({ p, denoms }: { p: DaoProposal; denoms?: Map<string, TokenSummary> }) {
  if (p.deposit_refunded == null) return null;
  const amount = p.deposit_amount ? coinsText(parseCoins(p.deposit_amount), denoms) : null;
  return (
    <Flex gap="1.5" align="center" wrap="wrap" data-testid="dao-deposit" data-refunded={p.deposit_refunded}>
      <Text as="span">
        Deposit{amount ? ` of ${amount}` : ""}:{" "}
        {p.deposit_refunded ? "refunded to the proposer" : "kept: sent to"}
      </Text>
      {!p.deposit_refunded && p.deposit_destination && <Destination value={p.deposit_destination} />}
    </Flex>
  );
}

/** How a settled proposal ended; what can still happen to a failed one. */
function Outcome({ p, denoms }: { p: DaoProposal; denoms?: Map<string, TokenSummary> }) {
  if (p.status === "voting" || p.status == null) return null;
  const tx = p.settled_tx_hash;
  const failure = p.execution_error;
  let body: ReactNode;
  switch (p.status) {
    case "executed":
      body = (
        <Text data-testid="outcome-text">
          The proposal passed and its messages ran as the DAO{p.expedited_early ? ", settled early" : ""}
          {tx && <> in transaction {txLink(tx)}</>}.
        </Text>
      );
      break;
    case "execution_failed":
      body = (
        <Stack gap="2">
          <Text data-testid="outcome-text">
            The proposal passed, but running its messages failed
            {tx && <> in transaction {txLink(tx)}</>}.
          </Text>
          {failure && (
            <Text fontFamily="mono" fontSize="12px" overflowWrap="anywhere" data-testid="execution-error">
              {failure}
            </Text>
          )}
          {p.retry_deadline && (
            <Text color="fg.muted" data-testid="retry-deadline">
              Anyone can retry it until {formatDateTime(p.retry_deadline, { seconds: false })}.
            </Text>
          )}
        </Stack>
      );
      break;
    case "rejected":
      body = (
        <Text data-testid="outcome-text">
          The proposal did not pass
          {tx && <> (settled in transaction {txLink(tx)})</>}: see the checks above.
        </Text>
      );
      break;
    case "expired":
      body = (
        <Stack gap="2">
          <Text data-testid="outcome-text">
            The proposal expired: its messages failed to run and nobody retried before the deadline
            {tx && <> (last attempt in transaction {txLink(tx)})</>}.
          </Text>
          {failure && (
            <Text fontFamily="mono" fontSize="12px" overflowWrap="anywhere" data-testid="execution-error">
              {failure}
            </Text>
          )}
        </Stack>
      );
      break;
    default:
      body = (
        <Text data-testid="outcome-text">
          The proposer cancelled the proposal{p.cancelled_height != null && <> in block {blockLink(p.cancelled_height)}</>}.
        </Text>
      );
  }
  return (
    <Section id="dao-outcome-title" title="Outcome" data-testid="dao-outcome">
      <Stack gap="3">
        {body}
        <Deposit p={p} denoms={denoms} />
      </Stack>
    </Section>
  );
}

function VoteRow({ v, p }: { v: DaoVote; p: DaoProposal }) {
  const stake = p.weight_denom != null || p.dao?.type === "stake";
  const reduced = isReduced(v);
  return (
    <Grid
      role="listitem"
      templateColumns={VOTE_COLUMNS}
      gap="3"
      alignItems="center"
      py="9px"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      fontSize="13px"
      data-testid="dao-vote-row"
    >
      <Box minW="0">
        <AccountChip address={v.voter} label={v.voter_label} />
      </Box>
      <Text fontWeight="600" color={OPTION_COLOR[v.option] ?? "fg.muted"}>
        {OPTION_TEXT[v.option] ?? v.option}
      </Text>
      <Box hideBelow="c2" minW="0" fontFamily="mono" data-testid="dao-vote-weight">
        {weightText(v.weight, p.weight_denom, true)}
        {reduced && (
          <Text as="span" display="block" fontFamily="body" fontSize="12px" color="warn.fg" data-testid="vote-reduced">
            counts {weightText(v.effective_weight, p.weight_denom, true)}: {stake ? REDUCED_NOTE_STAKE : REDUCED_NOTE_MEMBER}
          </Text>
        )}
      </Box>
      <Box hideBelow="c2" textAlign="end" fontSize="12px" color="fg.muted" whiteSpace="nowrap">
        {v.tx_hash ? (
          <Link asChild color="fg.muted">
            <NextLink href={`/txs/${v.tx_hash.toUpperCase()}`}>
              <TimeAgo time={v.time} />
            </NextLink>
          </Link>
        ) : (
          <TimeAgo time={v.time} />
        )}
      </Box>
      {/* Phones: the weight and time under the voter, the grid's second row. */}
      <Box hideFrom="c2" gridColumn="1 / -1" fontSize="12px" color="fg.muted" data-testid="dao-vote-phone">
        <Mono>{weightText(v.weight, p.weight_denom, true)}</Mono> · <TimeAgo time={v.time} />
        {reduced && (
          <Text as="span" display="block" color="warn.fg">
            counts {weightText(v.effective_weight, p.weight_denom, true)}: {stake ? REDUCED_NOTE_STAKE : REDUCED_NOTE_MEMBER}
          </Text>
        )}
      </Box>
    </Grid>
  );
}

function VoteHead() {
  return (
    <Grid templateColumns={VOTE_COLUMNS} gap="3" py="2" borderBottomWidth="1px" borderColor="border.panel" textStyle="tableHead" aria-hidden>
      <Text>Voter</Text>
      <Text>Vote</Text>
      <Text hideBelow="c2">Weight</Text>
      <Text hideBelow="c2" textAlign="end">
        Time
      </Text>
    </Grid>
  );
}

function VotesTab({ p }: { p: DaoProposal }) {
  const [option, setOption] = useState<DaoVoteOption | null>(null);
  const votes = useDaoProposalVotes(p.id, option, p.status === "voting");
  const items = votes.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <Box data-testid="dao-votes">
      <FilterPills label="Show votes" options={OPTION_FILTERS} value={option} onChange={setOption} mb="1.5" />
      {!votes.data ? (
        votes.error ? (
          <Text color="fg.muted" py="2" data-testid="dao-votes-error">
            The votes could not be loaded.
          </Text>
        ) : (
          <ListSkeleton rows={5} />
        )
      ) : items.length === 0 ? (
        <Text color="fg.muted" py="2" data-testid="dao-votes-empty">
          No votes here.
        </Text>
      ) : (
        <>
          <VoteHead />
          <Box role="list" aria-label="Votes">
            {items.map((v) => (
              <VoteRow key={v.voter} v={v} p={p} />
            ))}
          </Box>
        </>
      )}
      {votes.isFetchingNextPage && <ListSkeleton rows={3} />}
      {votes.hasNextPage && !votes.isFetchingNextPage && (
        <Button variant="outline" size="sm" mt="2.5" onClick={() => void votes.fetchNextPage()}>
          Show more votes
        </Button>
      )}
    </Box>
  );
}

/** A found proposal. */
export function DaoProposalView({ p, initialTab }: { p: DaoProposal; initialTab?: string }) {
  const id = useId();
  const denoms = useDenoms();
  const [tab, setTab] = useState<VoteTab>(parseTab(initialTab));
  const now = new Date();
  return (
    <Box>
      <Header p={p} />
      <SummaryTiles p={p} now={now} />
      <VTabsBar>
        <VTabs
          id={id}
          label="Proposal"
          value={tab}
          onChange={(t) => {
            setTab(t);
            writeTab(t);
          }}
          tabs={[
            { value: "overview", label: "Overview" },
            { value: "votes", label: "Votes", count: formatCount(p.votes_count) },
          ]}
        />
      </VTabsBar>
      <VTabPanel id={id} value={tab}>
        {tab === "overview" ? (
          <Stack gap="5">
            <Result p={p} />
            <Outcome p={p} denoms={denoms.data} />
            <WhatItDoes p={p} denoms={denoms.data} />
          </Stack>
        ) : (
          <VotesTab p={p} />
        )}
      </VTabPanel>
      <Box mt="6">
        <MoreDetails hint="raw JSON of the proposal">
          <JsonBlock value={p} />
        </MoreDetails>
      </Box>
    </Box>
  );
}

export function DaoProposalSkeleton() {
  return (
    <Box aria-busy data-testid="dao-proposal-skeleton">
      <Stack gap="3" pt="6" pb="5">
        <Skeleton h="3" w="28" />
        <Skeleton h="4" w="48" />
        <Skeleton h="7" w="96" maxW="full" />
      </Stack>
      <Grid templateColumns={TILES} gap="3" mb="6">
        {Array.from({ length: 5 }).map((_, i) => (
          <TileSkeleton key={i} />
        ))}
      </Grid>
      <Stack gap="5">
        <CardSkeleton lines={5} />
        <CardSkeleton lines={4} />
      </Stack>
    </Box>
  );
}

function MissingHead({ id, daoId }: { id: number; daoId: number | null }) {
  return (
    <Box pb="6">
      <Breadcrumb path={[{ label: "DAOs", href: "/daos" }, ...(daoId != null ? [{ label: `DAO #${daoId}`, href: daoHref(daoId) }] : [])]} current={`Proposal #${id}`} />
    </Box>
  );
}

/**
 * `/daos/[daoId]/proposals/[id]` (and `/dao-proposals/[id]` without a DAO in
 * the address): the skeleton, then the proposal (polled while voting), not
 * found on a 404 or when it belongs to another DAO, or the error state.
 */
export function DaoProposalPage({ id, daoId = null, initialTab }: { id: number; daoId?: number | null; initialTab?: string }) {
  const { data, error, refetch } = useDaoProposal(id);
  const elsewhere = data != null && daoId != null && data.dao_id != null && data.dao_id !== daoId;
  if (data && !elsewhere) return <DaoProposalView p={data} initialTab={initialTab} />;
  if (elsewhere || isNotFound(error)) {
    return (
      <Box>
        <MissingHead id={id} daoId={daoId} />
        <StateBox icon={<LuCircleHelp size={20} />} title="Proposal not found" data-testid="dao-proposal-not-found">
          The explorer does not know proposal #{id}
          {daoId != null ? ` in DAO #${daoId}` : ""}. Check the link, or find it in the{" "}
          <Link asChild color="accent.fg">
            <NextLink href={daoId != null ? daoHref(daoId) : "/daos"}>{daoId != null ? "DAO's proposals" : "DAOs list"}</NextLink>
          </Link>
          .
        </StateBox>
      </Box>
    );
  }
  if (error) {
    return (
      <Box>
        <MissingHead id={id} daoId={daoId} />
        <StateBox
          tone="error"
          icon={<LuTriangleAlert size={20} />}
          title="This proposal could not be loaded"
          actions={
            <Button size="sm" colorPalette="blue" onClick={() => void refetch()}>
              Try again
            </Button>
          }
          data-testid="dao-proposal-error"
        >
          The explorer did not answer. Try again in a moment.
        </StateBox>
      </Box>
    );
  }
  return <DaoProposalSkeleton />;
}
