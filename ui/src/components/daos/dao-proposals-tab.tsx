"use client";

import { Box, Button, Flex, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useState } from "react";
import { LuTriangleAlert } from "react-icons/lu";

import { useDaoProposals } from "@/api/hooks";
import type { Dao, DaoProposalStatus, DaoProposalSummary } from "@/api/types";
import { FilterPills } from "@/components/ui/filter-pills";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { TableSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { formatDate } from "@/lib/format";
import { daoProposalHref, STATUS_FILTERS } from "@/render/dao-proposals";
import { endsIn } from "@/render/proposals";

import { DaoStatusTag, DaoTallyBars } from "./proposal-parts";

// Title · Status · Ends; the end column shows from c2, on phones it sits under the title.
const COLUMNS = { base: "minmax(0, 1fr) auto", c2: "minmax(0, 1fr) 150px 120px" };

/** `ends in 3 days` while voting, `ended Nov 4, 2099` after; nothing for a proposal that never voted. */
function endText(p: DaoProposalSummary, now: Date): string | null {
  if (!p.voting_end) return null;
  if (p.status === "voting") {
    const left = endsIn(p.voting_end, now);
    return left === "ended" ? "voting ended" : `ends ${left}`;
  }
  if (p.status === "cancelled") return null;
  return `ended ${formatDate(p.voting_end)}`;
}

function Row({ p, daoId, now }: { p: DaoProposalSummary; daoId: number; now: Date }) {
  const end = endText(p, now);
  return (
    <ListRow templateColumns={COLUMNS} role="listitem" fontSize="13px" alignItems="start" data-testid="dao-proposal-row" data-status={p.status ?? "none"}>
      <Box minW="0">
        <Link asChild color="accent.fg" overflowWrap="anywhere">
          <NextLink href={daoProposalHref(p.dao_id ?? daoId, p.id)}>
            <Text as="span" fontFamily="mono" color="fg.muted" mr="1.5">
              #{p.id}
            </Text>
            {p.title || "Untitled proposal"}
          </NextLink>
        </Link>
        {p.expedited && (
          <Text as="span" ml="2" fontSize="12px" color="fg.muted">
            expedited
          </Text>
        )}
        {end && (
          <Text hideFrom="c2" mt="1" fontSize="12px" color="fg.muted" data-testid="dao-proposal-end-phone">
            {end}
          </Text>
        )}
        {p.status === "voting" && (
          <Box mt="2" data-testid="dao-proposal-live-bar">
            <DaoTallyBars p={p} />
          </Box>
        )}
      </Box>
      <Box>
        <DaoStatusTag status={p.status} />
      </Box>
      <Text hideBelow="c2" color="fg.muted" data-testid="dao-proposal-end">
        {end ?? "—"}
      </Text>
    </ListRow>
  );
}

/** The DAO's proposals, newest first, with the status chips; voting rows carry the live vote bar. */
export function DaoProposalsTab({ dao }: { dao: Dao }) {
  const [status, setStatus] = useState<DaoProposalStatus | null>(null);
  const list = useDaoProposals(dao.id, status);
  const now = new Date();
  const items = list.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <Box data-testid="dao-proposals">
      <FilterPills label="Filter by status" options={STATUS_FILTERS} value={status} onChange={setStatus} />
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
            data-testid="dao-proposals-error"
          >
            The explorer did not answer. Try again in a moment.
          </StateBox>
        ) : (
          <TableSkeleton rows={4} columns={3} />
        )
      ) : (
        <ListPanel>
          <ListHead templateColumns={COLUMNS} aria-hidden>
            <Text>Proposal</Text>
            <Text>Status</Text>
            <Text hideBelow="c2">Voting</Text>
          </ListHead>
          {items.length === 0 ? (
            <Text px="4" py="6" color="fg.muted" fontSize="13px" data-testid="dao-proposals-empty">
              {status ? "No proposals with this status." : "This DAO has no proposals yet."}
            </Text>
          ) : (
            <Box role="list" aria-label="Proposals">
              {items.map((p) => (
                <Row key={p.id} p={p} daoId={dao.id} now={now} />
              ))}
            </Box>
          )}
        </ListPanel>
      )}
      {list.isFetchingNextPage && (
        <Box mt="3">
          <TableSkeleton rows={3} columns={3} />
        </Box>
      )}
      {list.hasNextPage && !list.isFetchingNextPage && (
        <Flex mt="3">
          <Button variant="outline" size="sm" onClick={() => void list.fetchNextPage()}>
            Show older proposals
          </Button>
        </Flex>
      )}
    </Box>
  );
}
