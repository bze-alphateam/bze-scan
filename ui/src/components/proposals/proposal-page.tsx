"use client";

import { Box, Button, Grid, Heading, HStack, Link, Skeleton, Stack, Tabs, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useState, type ReactNode } from "react";
import { LuExternalLink } from "react-icons/lu";

import { isNotFound } from "@/api/client";
import {
  useAllValidators,
  useDenoms,
  useParams,
  useProposal,
  useProposalDeposits,
  useProposalVotes,
  useStats,
  useStatus,
  useValidatorVotes,
} from "@/api/hooks";
import type { Proposal, ProposalVote, TokenSummary, VoteOption } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Card } from "@/components/ui/card";
import { JsonBlock } from "@/components/ui/json-block";
import { MoreDetails } from "@/components/ui/more-details";
import { CardSkeleton, ListSkeleton } from "@/components/ui/skeleton";
import { TimeAgo } from "@/components/ui/time-ago";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount, formatDateTime, formatPct } from "@/lib/format";
import { pingPubProposal, voteDestinations } from "@/lib/links";
import {
  checks,
  coinsText,
  depositsReturned,
  describeProposal,
  endsIn,
  govRules,
  isOpen,
  kindLabel,
  metadataView,
  notVoted,
  optionText,
  resultLine,
  timeline,
  upgradeEta,
  verdict,
  votesSummary,
  type Action,
  type Checks,
} from "@/render/proposals";

import { ChecksList, ProposalStatusBadge, TallyBars, VerdictChip } from "./parts";
import { ActionView } from "./what-it-does";

const SUMMARY_FOLD = 600;
const listItem = (i: number) => ({ borderTopWidth: i === 0 ? "0" : "1px", borderColor: "border.panel" }) as const;

function Breadcrumb({ id }: { id: number }) {
  return (
    <HStack as="nav" aria-label="Breadcrumb" gap="2" fontSize="sm" color="fg.muted">
      <Link asChild color="fg.muted">
        <NextLink href="/proposals">Proposals</NextLink>
      </Link>
      <Text as="span" aria-hidden>
        /
      </Text>
      <Text as="span" aria-current="page">
        #{id}
      </Text>
    </HStack>
  );
}

function Header({ p, now }: { p: Proposal; now: Date }) {
  return (
    <Stack gap="3">
      <Breadcrumb id={p.id} />
      <HStack gap="2" wrap="wrap" fontSize="sm" color="fg.muted">
        <ProposalStatusBadge p={p} now={now} />
        <Text as="span">
          {kindLabel(p.kind)}
          {p.expedited ? " · expedited" : ""} · #{p.id}
        </Text>
      </HStack>
      <Heading as="h1" size={{ base: "2xl", md: "3xl" }} wordBreak="break-word">
        {p.title}
      </Heading>
      <HStack gap="1.5" wrap="wrap" fontSize="sm" color="fg.muted" data-testid="proposal-meta">
        {p.proposer && (
          <>
            <Text as="span">Proposed by</Text>
            <AccountChip address={p.proposer} label={p.proposer_label} />
            <Text as="span">·</Text>
          </>
        )}
        <Text as="span">Submitted {formatDateTime(p.submit_time)}</Text>
      </HStack>
      {p.status === "voting_period" && p.voting_end_time && (
        <Text fontSize="sm" fontWeight="medium" data-testid="proposal-when">
          Voting ends {endsIn(p.voting_end_time, now)} · {formatDateTime(p.voting_end_time)}
        </Text>
      )}
      {!isOpen(p.status) && p.voting_start_time && p.voting_end_time && (
        <Stack gap="0.5" fontSize="sm" data-testid="proposal-when">
          <Text color="fg.muted">
            Voting {formatDateTime(p.voting_start_time)} to {formatDateTime(p.voting_end_time)}
          </Text>
          <Text fontWeight="medium" data-testid="result-line">
            {resultLine(p, now)}
          </Text>
        </Stack>
      )}
    </Stack>
  );
}

function FromProposer({ p }: { p: Proposal }) {
  const [open, setOpen] = useState(false);
  const summary = p.summary?.trim() ?? "";
  const long = summary.length > SUMMARY_FOLD;
  const meta = metadataView(p.metadata);
  if (!summary && !meta) return null;
  return (
    <Stack gap="2" data-testid="from-proposer">
      <Heading as="h3" size="sm">
        From the proposer
      </Heading>
      {summary && (
        <Text fontSize="sm" whiteSpace="pre-line" wordBreak="break-word">
          {long && !open ? `${summary.slice(0, SUMMARY_FOLD).trimEnd()}…` : summary}
        </Text>
      )}
      {long && (
        <Button variant="ghost" size="sm" minH="44px" alignSelf="start" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
          {open ? "Show less" : "Read the full text"}
        </Button>
      )}
      {meta &&
        ("href" in meta ? (
          <Link href={meta.href} target="_blank" rel="noopener noreferrer nofollow" fontSize="sm" wordBreak="break-all">
            {meta.href} <LuExternalLink aria-hidden />
          </Link>
        ) : (
          <Text fontSize="sm" color="fg.muted" wordBreak="break-word">
            {meta.text}
          </Text>
        ))}
    </Stack>
  );
}

function WhatItDoes({ p, actions, denoms, eta }: { p: Proposal; actions: Action[]; denoms?: Map<string, TokenSummary>; eta: Date | null }) {
  const shown = actions.filter((a) => a.type !== "text");
  return (
    <Card title={shown.length > 0 ? "What this proposal does" : "What this proposal asks"} data-testid="what-it-does">
      <Stack gap="5">
        {shown.length === 0 && (
          <Text fontSize="sm" color="fg.muted">
            A text proposal: it changes nothing on the chain by itself.
          </Text>
        )}
        {shown.map((a, i) => (
          <ActionView key={i} a={a} p={p} denoms={denoms} upgradeEta={eta} />
        ))}
        <FromProposer p={p} />
      </Stack>
    </Card>
  );
}

const MEANINGS: [string, string][] = [
  ["Yes", "you want the change."],
  ["No", "you do not want it."],
  ["Abstain", "you count toward the turnout without taking a side."],
  ["No with veto", "you think it is harmful or spam. Too many vetoes reject it and burn its deposit."],
];

function Votes({ p, c, denoms }: { p: Proposal; c: Checks | null; denoms?: Map<string, TokenSummary> }) {
  const voting = p.status === "voting_period";
  if (p.status === "deposit_period" || p.status === "canceled" || p.status === "dropped") {
    return (
      <Card title="Votes">
        <Text fontSize="sm" color="fg.muted">
          {p.status === "canceled"
            ? "The proposer withdrew this proposal before voting ended."
            : p.status === "dropped"
              ? "The deposit was not reached in time, so voting never opened."
              : "Voting opens once the deposit is reached."}
        </Text>
      </Card>
    );
  }
  return (
    <Card
      title={
        <HStack justify="space-between" wrap="wrap" gap="2">
          <span>{voting ? "Votes · can change until voting ends" : "Final result"}</span>
          {voting && c && <VerdictChip v={verdict(c)} />}
        </HStack>
      }
      data-testid="votes-card"
    >
      <Stack gap="4">
        {p.tally ? <TallyBars tally={p.tally} denoms={denoms} amounts /> : <Text fontSize="sm" color="fg.muted">Not tallied yet.</Text>}
        {c ? (
          <ChecksList c={c} />
        ) : (
          p.tally && (
            <Text fontSize="sm" color="fg.muted">
              The voting rules could not be loaded, so the checks are not shown.
            </Text>
          )
        )}
        <Text fontSize="sm" color="fg.muted">
          A validator&apos;s vote counts for everyone staking with it, unless the staker votes themselves.{" "}
          {formatCount(p.votes_count)} {p.votes_count === 1 ? "account has" : "accounts have"} voted{voting ? " so far" : ""}.
        </Text>
        <Stack gap="1" fontSize="sm">
          <Text fontWeight="medium">What the options mean</Text>
          {MEANINGS.map(([o, m]) => (
            <Text key={o} color="fg.muted">
              <Text as="span" color="fg" fontWeight="medium">
                {o}
              </Text>
              : {m}
            </Text>
          ))}
        </Stack>
      </Stack>
    </Card>
  );
}

function WantToVote({ p }: { p: Proposal }) {
  return (
    <Card title="Want to vote?" data-testid="want-to-vote">
      <Stack gap="3" fontSize="sm">
        <Text>
          The explorer only shows the vote. Vote from a wallet with the account you stake from: your vote then counts for
          your stake instead of your validator&apos;s, and you can change it until voting ends.
        </Text>
        <Stack gap="2" role="list" aria-label="Where to vote">
          {voteDestinations(p.id).map((d) => (
            <Box key={d.href} role="listitem">
              <Button asChild colorPalette="blue" minH="44px">
                <a href={d.href} target="_blank" rel="noopener noreferrer">
                  {d.label} <LuExternalLink aria-hidden />
                </a>
              </Button>
            </Box>
          ))}
        </Stack>
        <Text color="fg.muted">More places to vote will be listed here as they open.</Text>
      </Stack>
    </Card>
  );
}

function VoteRow({ v, i, validator }: { v: ProposalVote; i: number; validator?: boolean }) {
  return (
    <Grid
      role="listitem"
      templateColumns={{ base: "minmax(0, 1fr) auto", md: validator ? "minmax(0, 1fr) 120px 140px 120px" : "minmax(0, 1fr) 120px 120px" }}
      gap={{ base: "1", md: "3" }}
      alignItems="center"
      py="2"
      minH="44px"
      fontSize="sm"
      {...listItem(i)}
      data-testid={validator ? "validator-vote" : "vote-row"}
    >
      <Box minW="0">
        {validator && v.validator_operator ? (
          <ValidatorChip operator={v.validator_operator} moniker={v.validator} />
        ) : (
          <AccountChip address={v.voter} label={v.voter_label ?? (v.validator ? { name: v.validator, kind: "validator_owner" } : null)} />
        )}
      </Box>
      <Text fontWeight="medium" justifySelf={{ base: "end", md: "start" }}>
        {optionText(v.option)}
      </Text>
      {validator && (
        <Text color="fg.muted" gridColumn={{ base: "1 / -1", md: "auto" }}>
          {v.voting_power_pct != null ? `${formatPct(v.voting_power_pct)} of all stake` : "—"}
        </Text>
      )}
      <HStack gap="1" color="fg.muted" fontSize="xs" gridColumn={{ base: "1 / -1", md: "auto" }}>
        {v.tx_hash ? (
          <Link asChild color="fg.muted">
            <NextLink href={`/txs/${v.tx_hash.toUpperCase()}`}>
              <TimeAgo time={v.time} />
            </NextLink>
          </Link>
        ) : (
          <TimeAgo time={v.time} />
        )}
      </HStack>
    </Grid>
  );
}

const NOT_VOTED_SHOWN = 5;

function ValidatorsTab({ p, votes }: { p: Proposal; votes: ProposalVote[] | undefined }) {
  const validators = useAllValidators();
  const [all, setAll] = useState(false);
  if (!votes) return <ListSkeleton rows={5} />;
  const missing = validators.data ? notVoted(validators.data.items, votes) : [];
  const shown = all ? missing : missing.slice(0, NOT_VOTED_SHOWN);
  const voting = p.status === "voting_period";
  return (
    <Stack gap="3">
      {votes.length === 0 ? (
        <Text fontSize="sm" color="fg.muted">
          No validator has voted{voting ? " yet" : ""}.
        </Text>
      ) : (
        <Stack gap="0" role="list" aria-label="Validator votes">
          {votes.map((v, i) => (
            <VoteRow key={v.voter} v={v} i={i} validator />
          ))}
        </Stack>
      )}
      {missing.length > 0 && (
        <HStack gap="1.5" wrap="wrap" fontSize="sm" color="fg.muted" data-testid="not-voted">
          <Text as="span">
            {missing.length} {missing.length === 1 ? "validator has" : "validators have"} {voting ? "not voted yet" : "not voted"}:
          </Text>
          {shown.map((v) => (
            <ValidatorChip key={v.operator_address} operator={v.operator_address} moniker={v.moniker} />
          ))}
          {missing.length > shown.length && (
            <Button variant="ghost" size="xs" minH="44px" onClick={() => setAll(true)}>
              Show all
            </Button>
          )}
          {voting && <Text as="span">· their stakers can still vote for themselves</Text>}
        </HStack>
      )}
    </Stack>
  );
}

const VOTE_FILTERS: { label: string; value: VoteOption | "weighted" | null }[] = [
  { label: "All", value: null },
  { label: "Yes", value: "yes" },
  { label: "No", value: "no" },
  { label: "Abstain", value: "abstain" },
  { label: "Veto", value: "no_with_veto" },
  { label: "Split", value: "weighted" },
];

function EveryoneTab({ p }: { p: Proposal }) {
  const [option, setOption] = useState<VoteOption | "weighted" | null>(null);
  const votes = useProposalVotes(p.id, option, isOpen(p.status));
  const items = votes.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <Stack gap="3">
      <HStack gap="2" wrap="wrap" role="group" aria-label="Show votes">
        {VOTE_FILTERS.map((f) => (
          <Button
            key={f.label}
            size="xs"
            minH="44px"
            rounded="full"
            variant={option === f.value ? "subtle" : "outline"}
            aria-pressed={option === f.value}
            onClick={() => setOption(f.value)}
          >
            {f.label}
          </Button>
        ))}
      </HStack>
      {!votes.data ? (
        votes.error ? (
          <Text fontSize="sm" color="fg.muted">
            The votes could not be loaded.
          </Text>
        ) : (
          <ListSkeleton rows={5} />
        )
      ) : items.length === 0 ? (
        <Text fontSize="sm" color="fg.muted">
          No votes here.
        </Text>
      ) : (
        <Stack gap="0" role="list" aria-label="Votes">
          {items.map((v, i) => (
            <VoteRow key={v.voter} v={v} i={i} />
          ))}
        </Stack>
      )}
      {votes.isFetchingNextPage && <ListSkeleton rows={5} />}
      {votes.hasNextPage && !votes.isFetchingNextPage && (
        <Button variant="outline" size="sm" minH="44px" alignSelf="start" onClick={() => void votes.fetchNextPage()}>
          Show more votes
        </Button>
      )}
    </Stack>
  );
}

function WhoVoted({ p }: { p: Proposal }) {
  const open = isOpen(p.status);
  const validatorVotes = useValidatorVotes(p.id, open);
  const validators = useAllValidators();
  const [expanded, setExpanded] = useState(open);
  const missing = validatorVotes.data && validators.data ? notVoted(validators.data.items, validatorVotes.data).length : null;
  return (
    <Card title="Who voted" data-testid="who-voted">
      <Stack gap="3">
        {!open && (
          <Text fontSize="sm" data-testid="votes-summary">
            {validatorVotes.data && missing != null ? votesSummary(validatorVotes.data, missing, p.votes_count) : <Skeleton as="span" h="4" w="64" display="inline-block" />}
          </Text>
        )}
        {!expanded ? (
          <Button variant="outline" size="sm" minH="44px" alignSelf="start" onClick={() => setExpanded(true)}>
            See every vote
          </Button>
        ) : (
          <Tabs.Root defaultValue="validators" variant="line" lazyMount>
            <Tabs.List>
              <Tabs.Trigger value="validators" minH="44px">
                Validators · {p.validators_voted.voted} of {p.validators_voted.total}
              </Tabs.Trigger>
              <Tabs.Trigger value="everyone" minH="44px">
                Everyone · {formatCount(p.votes_count)}
              </Tabs.Trigger>
            </Tabs.List>
            <Tabs.Content value="validators" pt="3">
              <ValidatorsTab p={p} votes={validatorVotes.data} />
            </Tabs.Content>
            <Tabs.Content value="everyone" pt="3">
              <EveryoneTab p={p} />
            </Tabs.Content>
          </Tabs.Root>
        )}
      </Stack>
    </Card>
  );
}

function Timeline({ items }: { items: ReturnType<typeof timeline> }) {
  return (
    <Card title="Timeline">
      <Stack gap="0" role="list" aria-label="Timeline">
        {items.map((item) => (
          <HStack key={item.label} role="listitem" gap="3" align="start" py="2" data-testid="timeline-item" data-done={item.done}>
            <Box mt="1.5" boxSize="2.5" rounded="full" flexShrink="0" bg={item.done ? "blue.solid" : "transparent"} borderWidth="2px" borderColor="blue.solid" aria-hidden />
            <Stack gap="0" fontSize="sm" minW="0">
              <Text fontWeight="medium">{item.label}</Text>
              <Text color="fg.muted" fontSize="xs">
                {item.time && `${item.done ? "" : "about "}${formatDateTime(item.time)}`}
                {item.height != null && (
                  <>
                    {item.time ? " · " : ""}block{" "}
                    <Link asChild color="fg.muted">
                      <NextLink href={`/blocks/${item.height}`}>{formatCount(item.height)}</NextLink>
                    </Link>
                  </>
                )}
              </Text>
            </Stack>
          </HStack>
        ))}
      </Stack>
    </Card>
  );
}

/** A label/value row of a details list; stacks on phones. */
function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Grid templateColumns={{ base: "minmax(0, 1fr)", md: "180px minmax(0, 1fr)" }} gap={{ base: "0.5", md: "4" }} py="2">
      <Text fontSize="sm" color="fg.muted">
        {label}
      </Text>
      <Box fontSize="sm" minW="0" wordBreak="break-word">
        {children}
      </Box>
    </Grid>
  );
}

function Details({ p, actions, vetoed, denoms }: { p: Proposal; actions: Action[]; vetoed: boolean; denoms?: Map<string, TokenSummary> }) {
  const deposits = useProposalDeposits(p.id);
  const returned = depositsReturned(p.status, vetoed);
  const upgrade = actions.find((a) => a.type === "upgrade");
  return (
    <Stack gap="4">
      <Card>
        <Stack gap="0">
          <Row label="Message types">
            <Stack gap="0.5">
              {p.message_types.length === 0 ? "none" : p.message_types.map((t, i) => (
                <Text key={`${t}-${i}`} fontFamily="mono" fontSize="xs" wordBreak="break-all">
                  {t}
                </Text>
              ))}
            </Stack>
          </Row>
          {upgrade && (
            <Row label="Upgrade">
              {upgrade.name}
              {upgrade.height != null && ` at block ${formatCount(upgrade.height)}`}
            </Row>
          )}
          <Row label="Deposits">
            {!deposits.data ? (
              deposits.error ? "could not be loaded" : <Skeleton h="4" w="48" />
            ) : deposits.data.length === 0 ? (
              "none indexed"
            ) : (
              <Stack gap="1" data-testid="deposits">
                {deposits.data.map((d, i) => (
                  <HStack key={`${d.depositor}-${d.height}-${i}`} gap="1.5" wrap="wrap">
                    <Text as="span">{coinsText(d.amount, denoms)} by</Text>
                    <AccountChip address={d.depositor} label={d.depositor_label} />
                    {returned && <Text as="span" color="fg.muted">· returned</Text>}
                  </HStack>
                ))}
              </Stack>
            )}
          </Row>
          <Row label="Tally source">
            {isOpen(p.status) ? "Live from the chain, refreshed every minute" : "The chain's final tally"}
            {p.tally_updated_at && ` · as of ${formatDateTime(p.tally_updated_at)}`}
          </Row>
        </Stack>
      </Card>
      <Stack gap="2">
        <Heading as="h3" size="sm">
          Raw proposal
        </Heading>
        <JsonBlock value={p} />
      </Stack>
      <Link href={pingPubProposal(p.id)} target="_blank" rel="noopener noreferrer" fontSize="sm" alignSelf="start">
        Open on Ping.pub <LuExternalLink aria-hidden />
      </Link>
    </Stack>
  );
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
  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Header p={p} now={now} />
      <Grid templateColumns={{ base: "minmax(0, 1fr)", lg: "minmax(0, 1.6fr) minmax(0, 1fr)" }} gap="6" alignItems="start">
        <Stack gap="6" minW="0">
          <WhatItDoes p={p} actions={actions} denoms={denoms.data} eta={eta} />
          <WhoVoted p={p} />
        </Stack>
        <Stack gap="6" minW="0">
          <Votes p={p} c={c} denoms={denoms.data} />
          {p.status === "voting_period" && <WantToVote p={p} />}
          <Timeline items={items} />
        </Stack>
      </Grid>
      <MoreDetails hint="message types, deposits, raw proposal">
        <Details p={p} actions={actions} vetoed={c != null && !c.veto.ok} denoms={denoms.data} />
      </MoreDetails>
    </Stack>
  );
}

export function ProposalSkeleton() {
  return (
    <Stack gap="6" aria-busy data-testid="proposal-skeleton">
      <Stack gap="3">
        <Skeleton h="4" w="32" />
        <Skeleton h="6" w="48" />
        <Skeleton h="9" w="96" maxW="full" />
        <Skeleton h="4" w="72" maxW="full" />
      </Stack>
      <Grid templateColumns={{ base: "minmax(0, 1fr)", lg: "minmax(0, 1.6fr) minmax(0, 1fr)" }} gap="6">
        <Stack gap="6">
          <CardSkeleton lines={5} />
          <ListSkeleton rows={5} />
        </Stack>
        <Stack gap="2" p="5" borderWidth="1px" borderColor="border.panel" rounded="l3" aria-hidden>
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} h="3" w="full" data-testid="tally-skeleton" />
          ))}
        </Stack>
      </Grid>
    </Stack>
  );
}

/** `/proposals/[id]`: the skeleton, then the proposal (polled while open), not found on a 404, or the error state. */
export function ProposalPage({ id }: { id: number }) {
  const { data, error, refetch } = useProposal(id);
  if (data) return <ProposalView p={data} />;
  if (isNotFound(error)) {
    return (
      <Card title="Proposal not found">
        <Text fontSize="sm" color="fg.muted">
          The explorer does not know proposal #{id}. Check the link, or find it in the{" "}
          <Link asChild>
            <NextLink href="/proposals">proposals list</NextLink>
          </Link>
          .
        </Text>
      </Card>
    );
  }
  if (error) {
    return (
      <Card title="This proposal could not be loaded">
        <HStack justify="space-between" wrap="wrap" gap="3">
          <Text color="fg.muted" fontSize="sm">
            The explorer did not answer. Try again in a moment.
          </Text>
          <Button colorPalette="blue" minH="44px" onClick={() => void refetch()}>
            Try again
          </Button>
        </HStack>
      </Card>
    );
  }
  return <ProposalSkeleton />;
}

