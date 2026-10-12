"use client";

import { Box, Button, Grid, Heading, HStack, Input, Link, Skeleton, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useState } from "react";
import { LuSearch } from "react-icons/lu";

import { useParams, useProposal, useProposals } from "@/api/hooks";
import type { ProposalStatus, ProposalSummary } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Card } from "@/components/ui/card";
import { TableSkeleton } from "@/components/ui/skeleton";
import { formatDateTime, formatPct } from "@/lib/format";
import { checks, endedText, endsIn, excerpt, govRules, kindLabel, resultText, verdict } from "@/render/proposals";

import { ProposalStatusBadge, TallyBars, VerdictChip } from "./parts";

const proposalHref = (id: number) => `/proposals/${id}`;

/** One proposal in its voting period: what it is, the votes so far and where it is heading. */
function VotingCard({ p, now }: { p: ProposalSummary; now: Date }) {
  const detail = useProposal(p.id);
  const params = useParams();
  const rules = govRules(params.data, p.expedited);
  const c = checks(p, rules);
  return (
    <Card data-testid="voting-card">
      <Stack gap="3">
        <Stack gap="1">
          <Text fontSize="sm" color="fg.muted">
            #{p.id} · {kindLabel(p.kind)}
            {p.expedited ? " · expedited" : ""}
          </Text>
          <Heading as="h3" size="md">
            <Link asChild>
              <NextLink href={proposalHref(p.id)}>{p.title}</NextLink>
            </Link>
          </Heading>
          {detail.data?.summary && (
            <Text fontSize="sm" color="fg.muted">
              {excerpt(detail.data.summary, 240)}
            </Text>
          )}
        </Stack>
        <HStack gap="1.5" wrap="wrap" fontSize="sm" color="fg.muted">
          {detail.data?.proposer && (
            <>
              <Text as="span">by</Text>
              <AccountChip address={detail.data.proposer} label={detail.data.proposer_label} />
              <Text as="span">·</Text>
            </>
          )}
          {p.voting_end_time && (
            <Text as="span">
              ends {formatDateTime(p.voting_end_time)} ({endsIn(p.voting_end_time, now)})
            </Text>
          )}
        </HStack>
        <TallyBars tally={p.tally} />
        <HStack justify="space-between" wrap="wrap" gap="2">
          <Text fontSize="sm" color="fg.muted">
            {p.turnout_pct != null ? `${formatPct(p.turnout_pct)} of staked BZE voted` : "Not tallied yet"}
            {c && ` · quorum of ${formatPct(c.turnout.need, { trim: true })} ${c.turnout.ok ? "reached" : "not reached yet"}`}
          </Text>
          {c && <VerdictChip v={verdict(c)} />}
        </HStack>
      </Stack>
    </Card>
  );
}

function VotingNow({ now }: { now: Date }) {
  const voting = useProposals("voting_period");
  if (!voting.data) {
    return voting.error ? null : <VotingCardSkeleton />;
  }
  // The filter is the API's; checking it again keeps a stale page honest.
  const items = voting.data.pages.flatMap((page) => page.items).filter((p) => p.status === "voting_period");
  return (
    <Stack gap="3">
      <Heading as="h2" size="lg">
        Voting now
      </Heading>
      {items.length === 0 ? (
        <Text fontSize="sm" color="fg.muted">
          No proposal is being voted on right now.
        </Text>
      ) : (
        items.map((p) => <VotingCard key={p.id} p={p} now={now} />)
      )}
    </Stack>
  );
}

function VotingCardSkeleton() {
  return (
    <Stack gap="3" aria-hidden data-testid="voting-card-skeleton">
      <Skeleton h="6" w="32" />
      <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" p="5">
        <Stack gap="3">
          <Skeleton h="4" w="40" />
          <Skeleton h="6" w="72" maxW="full" />
          <Skeleton h="4" w="full" />
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} h="3" w="full" />
          ))}
        </Stack>
      </Box>
    </Stack>
  );
}

const FILTERS: { label: string; value: ProposalStatus | null }[] = [
  { label: "All", value: null },
  { label: "Voting", value: "voting_period" },
  { label: "Passed", value: "passed" },
  { label: "Rejected", value: "rejected" },
];

const COLUMNS = { base: "minmax(0, 1fr) auto", md: "56px minmax(0, 2.4fr) 128px 112px 88px 120px" };

/** A label shown before a value on phones, where the header row is hidden. */
function PhoneLabel({ children }: { children: string }) {
  return (
    <Text as="span" hideFrom="md" fontSize="xs" color="fg.muted" me="1">
      {children}
    </Text>
  );
}

function ProposalRow({ p, now }: { p: ProposalSummary; now: Date }) {
  const result = resultText(p.tally);
  const ended = endedText(p, now);
  return (
    <Grid
      role="listitem"
      templateColumns={COLUMNS}
      gap={{ base: "1", md: "4" }}
      alignItems="center"
      px="4"
      py="3"
      minH="56px"
      borderTopWidth="1px"
      borderColor="border.panel"
      data-testid="proposal-row"
    >
      <Text hideBelow="md" color="fg.muted" fontSize="sm">
        #{p.id}
      </Text>
      <Stack gap="0.5" minW="0">
        <Link asChild fontWeight="medium">
          <NextLink href={proposalHref(p.id)}>
            <Text as="span" hideFrom="md" color="fg.muted" me="1">
              #{p.id}
            </Text>
            {p.title}
          </NextLink>
        </Link>
        <Text fontSize="xs" color="fg.muted">
          {kindLabel(p.kind)}
        </Text>
      </Stack>
      <Box gridRow={{ base: "1", md: "auto" }} gridColumn={{ base: "2", md: "auto" }} justifySelf={{ base: "end", md: "start" }}>
        <ProposalStatusBadge p={p} now={now} />
      </Box>
      <Text fontSize="sm" gridColumn={{ base: "1 / -1", md: "auto" }}>
        <PhoneLabel>Result</PhoneLabel>
        {result ?? "—"}
      </Text>
      <Text fontSize="sm" gridColumn={{ base: "1 / -1", md: "auto" }}>
        <PhoneLabel>Turnout</PhoneLabel>
        {p.turnout_pct != null ? formatPct(p.turnout_pct) : "—"}
      </Text>
      <Text fontSize="sm" color="fg.muted" gridColumn={{ base: "1 / -1", md: "auto" }}>
        <PhoneLabel>Ended</PhoneLabel>
        {ended ?? "—"}
      </Text>
    </Grid>
  );
}

function AllProposals({ now }: { now: Date }) {
  const [status, setStatus] = useState<ProposalStatus | null>(null);
  const [query, setQuery] = useState("");
  const list = useProposals(status);
  const q = query.trim().toLowerCase();
  const items = (list.data?.pages.flatMap((page) => page.items) ?? []).filter(
    (p) => (status == null || p.status === status) && (q === "" || p.title.toLowerCase().includes(q)),
  );
  return (
    <Stack gap="3">
      <Heading as="h2" size="lg">
        All proposals
      </Heading>
      <HStack justify="space-between" wrap="wrap" gap="3">
        <HStack gap="2" wrap="wrap" role="group" aria-label="Show proposals">
          {FILTERS.map((f) => (
            <Button
              key={f.label}
              size="sm"
              minH="44px"
              rounded="full"
              variant={status === f.value ? "subtle" : "outline"}
              aria-pressed={status === f.value}
              onClick={() => setStatus(f.value)}
            >
              {f.label}
            </Button>
          ))}
        </HStack>
        <HStack gap="2" px="3" borderWidth="1px" borderColor="border.panel" rounded="l2" w={{ base: "full", sm: "64" }} color="fg.muted">
          <LuSearch aria-hidden />
          <Input
            aria-label="Find a proposal"
            placeholder="Find a proposal"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            variant="flushed"
            border="0"
            minH="44px"
          />
        </HStack>
      </HStack>
      {!list.data ? (
        list.error ? (
          <LoadError onRetry={() => void list.refetch()} />
        ) : (
          // A chip change is a different list: its rows load as skeletons.
          <TableSkeleton rows={10} columns={6} />
        )
      ) : (
        <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" overflow="hidden">
          <Grid
            hideBelow="md"
            templateColumns={COLUMNS}
            gap="4"
            px="4"
            h="10"
            alignItems="center"
            fontSize="xs"
            color="fg.muted"
            textTransform="uppercase"
            letterSpacing="wide"
            aria-hidden
          >
            <Text>#</Text>
            <Text>Proposal</Text>
            <Text>Status</Text>
            <Text>Result</Text>
            <Text>Turnout</Text>
            <Text>Ended</Text>
          </Grid>
          <Box role="list" aria-label="Proposals">
            {items.map((p) => (
              <ProposalRow key={p.id} p={p} now={now} />
            ))}
          </Box>
          {items.length === 0 && (
            <Text px="4" py="6" color="fg.muted" fontSize="sm" borderTopWidth="1px" borderColor="border.panel">
              {q ? "No proposal has that title." : "No proposals here yet."}
            </Text>
          )}
        </Box>
      )}
      {list.isFetchingNextPage && <TableSkeleton rows={5} columns={6} />}
      {list.hasNextPage && !list.isFetchingNextPage && (
        <Button variant="outline" size="sm" minH="44px" alignSelf="start" onClick={() => void list.fetchNextPage()}>
          Show older proposals
        </Button>
      )}
    </Stack>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <Card title="The proposals could not be loaded">
      <HStack justify="space-between" wrap="wrap" gap="3">
        <Text color="fg.muted" fontSize="sm">
          The explorer did not answer. Try again in a moment.
        </Text>
        <Button colorPalette="blue" minH="44px" onClick={onRetry}>
          Try again
        </Button>
      </HStack>
    </Card>
  );
}

/** `/proposals`: the live vote on top, then every proposal. */
export function ProposalsPage() {
  const now = new Date();
  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Stack gap="3">
        <Heading as="h1" size={{ base: "2xl", md: "3xl" }}>
          Proposals
        </Heading>
        <Text color="fg.muted" maxW="3xl">
          Changes to the chain are decided by vote. Anyone staking BZE can vote. If you don&apos;t, your validator&apos;s vote
          counts for your stake.{" "}
          <Link asChild>
            <NextLink href="/parameters#gov">See the voting rules</NextLink>
          </Link>
        </Text>
      </Stack>
      <VotingNow now={now} />
      <AllProposals now={now} />
    </Stack>
  );
}
