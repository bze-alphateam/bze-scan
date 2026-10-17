"use client";

import { Box, Button, Flex, Heading, Link, Skeleton, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useState } from "react";
import { LuTriangleAlert } from "react-icons/lu";

import { useParams, useProposal, useProposals } from "@/api/hooks";
import type { ProposalStatus, ProposalSummary } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { FilterPills } from "@/components/ui/filter-pills";
import { FindBox } from "@/components/ui/find-box";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { PageTitle, SectionHead } from "@/components/ui/page-head";
import { TableSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { formatDateTime, formatPct } from "@/lib/format";
import { checks, endedText, endsIn, excerpt, govRules, kindLabel, resultText, verdict } from "@/render/proposals";

import { HistoricalChip, ProposalStatusTag, TallyBars, VerdictTag } from "./parts";

const proposalHref = (id: number) => `/proposals/${id}`;

/** One proposal in its voting period: what it is, the votes so far and where it is heading. */
function VotingCard({ p, now }: { p: ProposalSummary; now: Date }) {
  const detail = useProposal(p.id);
  const params = useParams();
  const rules = govRules(params.data, p.expedited);
  const c = checks(p, rules);
  return (
    <Stack
      as="article"
      gap="10px"
      px="5"
      py="18px"
      bg="bg.panel"
      borderWidth="1px"
      borderColor="border.panel"
      borderLeftWidth="2px"
      borderLeftColor="accent.fg"
      rounded="l3"
      data-testid="voting-card"
    >
      <Text fontFamily="mono" fontSize="12px" color="fg.muted">
        #{p.id} · {kindLabel(p.kind)}
        {p.expedited ? " · expedited" : ""}
      </Text>
      <Heading as="h3" fontSize="18px" fontWeight="600" lineHeight="1.35">
        <Link asChild color="accent.fg">
          <NextLink href={proposalHref(p.id)}>{p.title}</NextLink>
        </Link>
      </Heading>
      {detail.data?.summary && (
        <Text fontSize="13px" lineHeight="1.55" color="fg.muted" maxW="860px">
          {excerpt(detail.data.summary, 240)}
        </Text>
      )}
      <Flex gap="1.5" align="center" wrap="wrap" fontSize="13px" color="fg.muted">
        {detail.data?.proposer && (
          <>
            <Text as="span">by</Text>
            <AccountChip address={detail.data.proposer} label={detail.data.proposer_label} />
            <Text as="span">·</Text>
          </>
        )}
        {p.voting_end_time && (
          <Text as="span">
            ends{" "}
            <Text as="span" fontFamily="mono">
              {formatDateTime(p.voting_end_time, { seconds: false })}
            </Text>{" "}
            ({endsIn(p.voting_end_time, now)})
          </Text>
        )}
      </Flex>
      <Box my="1">
        <TallyBars tally={p.tally} />
      </Box>
      <Flex justify="space-between" align="center" wrap="wrap" gap="10px" fontSize="13px">
        <Flex gap="1.5" align="center" wrap="wrap" color="fg.muted">
          <Text as="span">
            {p.turnout_pct != null ? (
              <>
                <Text as="span" fontFamily="mono">
                  {formatPct(p.turnout_pct)}
                </Text>{" "}
                of staked BZE voted
              </>
            ) : (
              "Not tallied yet"
            )}
            {c && c.turnout.ok !== null && (
              <>
                {" "}
                · quorum of{" "}
                <Text as="span" fontFamily="mono">
                  {formatPct(c.turnout.need, { trim: true })}
                </Text>{" "}
                {c.turnout.ok ? "reached" : "not reached yet"}
              </>
            )}
          </Text>
          <HistoricalChip p={p} />
        </Flex>
        {c && <VerdictTag v={verdict(c)} />}
      </Flex>
    </Stack>
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
    <Box as="section" mb="7">
      <SectionHead title="Voting now" />
      {items.length === 0 ? (
        <Text fontSize="13px" color="fg.muted">
          No proposal is being voted on right now.
        </Text>
      ) : (
        <Stack gap="3">
          {items.map((p) => (
            <VotingCard key={p.id} p={p} now={now} />
          ))}
        </Stack>
      )}
    </Box>
  );
}

function VotingCardSkeleton() {
  return (
    <Box mb="7" aria-hidden data-testid="voting-card-skeleton">
      <Skeleton h="3" w="24" mb="10px" />
      <Stack gap="10px" px="5" py="18px" bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3">
        <Skeleton h="3" w="40" />
        <Skeleton h="5" w="72" maxW="full" />
        <Skeleton h="3" w="full" />
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} h="1.5" w="full" maxW="720px" />
        ))}
      </Stack>
    </Box>
  );
}

const FILTERS: { label: string; value: ProposalStatus | null }[] = [
  { label: "All", value: null },
  { label: "Voting", value: "voting_period" },
  { label: "Passed", value: "passed" },
  { label: "Rejected", value: "rejected" },
];

const FILTER_WORDS: Partial<Record<ProposalStatus, string>> = { voting_period: "being voted on", passed: "passed", rejected: "rejected" };

// # (c2) · Proposal · Status · Result (c2) · Turnout (c3) · Ended (c2).
const COLUMNS = {
  base: "minmax(0, 1fr) auto",
  c2: "56px minmax(0, 1fr) 120px 100px 110px",
  c3: "56px minmax(0, 1fr) 120px 100px 150px 110px",
};

function ProposalRow({ p, now }: { p: ProposalSummary; now: Date }) {
  const result = resultText(p.tally);
  const ended = endedText(p, now);
  return (
    <ListRow role="listitem" templateColumns={COLUMNS} fontSize="13px" data-testid="proposal-row">
      <Text hideBelow="c2" fontFamily="mono" color="fg.muted">
        #{p.id}
      </Text>
      <Box minW="0">
        <Link asChild fontWeight="500" color="accent.fg">
          <NextLink href={proposalHref(p.id)}>
            <Text as="span" hideFrom="c2" fontFamily="mono" color="fg.muted" me="1">
              #{p.id}
            </Text>
            {p.title}
          </NextLink>
        </Link>
        <Text fontSize="12px" color="fg.muted">
          {kindLabel(p.kind)}
        </Text>
      </Box>
      <Box>
        <ProposalStatusTag p={p} now={now} />
      </Box>
      <Text hideBelow="c2" fontFamily="mono">
        {result ?? "—"}
      </Text>
      <Flex hideBelow="c3" gap="1.5" align="center" wrap="wrap" data-testid="turnout">
        <Text as="span" fontFamily="mono">
          {p.turnout_pct != null ? formatPct(p.turnout_pct) : "—"}
        </Text>
        <HistoricalChip p={p} />
      </Flex>
      <Text hideBelow="c2" color="fg.muted">
        {ended ?? "—"}
      </Text>
    </ListRow>
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
    <Box as="section">
      <SectionHead title="All proposals" />
      <Flex justify="space-between" align="center" wrap="wrap" gap="3" mb="14px">
        <FilterPills label="Filter by status" options={FILTERS} value={status} onChange={setStatus} mb="0" />
        <FindBox label="Find a proposal" value={query} onChange={setQuery} />
      </Flex>
      {!list.data ? (
        list.error ? (
          <StateBox
            tone="error"
            icon={<LuTriangleAlert size={20} />}
            title="The proposals could not be loaded"
            actions={
              <Button size="sm" colorPalette="blue" onClick={() => void list.refetch()}>
                Try again
              </Button>
            }
            data-testid="proposals-error"
          >
            The explorer did not answer. Try again in a moment.
          </StateBox>
        ) : (
          // A pill change is a different list: its rows load as skeletons.
          <TableSkeleton rows={10} columns={6} />
        )
      ) : (
        <ListPanel>
          <ListHead templateColumns={COLUMNS} aria-hidden>
            <Text hideBelow="c2">#</Text>
            <Text>Proposal</Text>
            <Text>Status</Text>
            <Text hideBelow="c2">Result</Text>
            <Text hideBelow="c3">Turnout</Text>
            <Text hideBelow="c2">Ended</Text>
          </ListHead>
          <Box role="list" aria-label="Proposals">
            {items.map((p) => (
              <ProposalRow key={p.id} p={p} now={now} />
            ))}
          </Box>
          {items.length === 0 && (
            <Text px="4" py="6" color="fg.muted" fontSize="13px">
              {q
                ? `No proposal matches “${query.trim()}”.`
                : status
                  ? `No proposal ${FILTER_WORDS[status] ?? ""} in this list.`
                  : "No proposals here yet."}
            </Text>
          )}
        </ListPanel>
      )}
      {list.isFetchingNextPage && (
        <Box mt="3">
          <TableSkeleton rows={5} columns={6} />
        </Box>
      )}
      {list.hasNextPage && !list.isFetchingNextPage && (
        <Box mt="3">
          <Button variant="outline" size="sm" onClick={() => void list.fetchNextPage()}>
            Show older proposals
          </Button>
        </Box>
      )}
    </Box>
  );
}

/** `/proposals`: the live vote on top, then every proposal. */
export function ProposalsPage() {
  const now = new Date();
  return (
    <Box>
      <PageTitle
        title="Proposals"
        intro={
          <>
            Changes to the chain are decided by vote. Anyone staking BZE can vote. If you don&apos;t, your validator&apos;s vote
            counts for your stake.{" "}
            <Link asChild color="accent.fg">
              <NextLink href="/parameters?group=gov">See the voting rules</NextLink>
            </Link>
          </>
        }
      />
      <VotingNow now={now} />
      <AllProposals now={now} />
    </Box>
  );
}
