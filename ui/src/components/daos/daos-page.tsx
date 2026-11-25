"use client";

import { Box, Button, Flex, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useState } from "react";
import { LuCircleHelp, LuTriangleAlert } from "react-icons/lu";

import { useDaos } from "@/api/hooks";
import type { DaoSummary, DaoType } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { ExternalLink } from "@/components/ui/external-link";
import { FilterPills } from "@/components/ui/filter-pills";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { PageTitle } from "@/components/ui/page-head";
import { TableSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { TimeAgo } from "@/components/ui/time-ago";
import { daoAppLinks } from "@/lib/links";
import { daoHref } from "@/render/daos";

import { OpenBadge, TypeTag } from "./parts";

const FILTERS: { label: string; value: DaoType | null }[] = [
  { label: "All", value: null },
  { label: "Member list", value: "static_list" },
  { label: "Stakers", value: "stake" },
];

// DAO · Type · Admin · Created. On phones the type tag sits under the name.
const COLUMNS = {
  base: "minmax(0, 1fr)",
  c2: "minmax(0, 1.6fr) 120px minmax(0, 1.3fr) 130px",
};

function DaoRow({ d }: { d: DaoSummary }) {
  return (
    <ListRow role="listitem" templateColumns={COLUMNS} fontSize="13px" data-testid="dao-row">
      <Box minW="0">
        <Flex gap="2" align="center" wrap="wrap">
          <Link asChild fontWeight="500" color="accent.fg">
            <NextLink href={daoHref(d.id)}>{d.name || `DAO #${d.id}`}</NextLink>
          </Link>
          <Text as="span" fontFamily="mono" fontSize="12px" color="fg.muted">
            #{d.id}
          </Text>
          <OpenBadge count={d.proposals_open} />
        </Flex>
        <Box hideFrom="c2" mt="1">
          <TypeTag type={d.type} />
        </Box>
      </Box>
      <Box hideBelow="c2">
        <TypeTag type={d.type} />
      </Box>
      <Box hideBelow="c2" minW="0" data-testid="dao-admin">
        {d.self_governed ? <Text color="fg.muted">Self-governed</Text> : <AccountChip address={d.admin} label={d.admin_label} />}
      </Box>
      <Text hideBelow="c2" color="fg.muted">
        {d.created_time ? <TimeAgo time={d.created_time} /> : "—"}
      </Text>
    </ListRow>
  );
}

/** `/daos`: every DAO of the chain, filtered by who votes in it. */
export function DaosPage() {
  const [type, setType] = useState<DaoType | null>(null);
  const list = useDaos(type);
  const items = list.data?.pages.flatMap((page) => page.items) ?? [];
  // Before chain v8.3.0 the API answers an empty list with supported = false.
  const supported = list.data ? list.data.pages[0]?.supported !== false : true;
  return (
    <Box>
      <PageTitle
        title="DAOs"
        intro="A DAO is a group with a shared treasury. Its members vote on what the DAO does, either as a fixed list of members or by the stake they hold."
      >
        <ExternalLink href={daoAppLinks.home} fontSize="13px" fontWeight="500" data-testid="dao-app-link">
          Open the DAO app
        </ExternalLink>
      </PageTitle>
      {!supported ? (
        <StateBox icon={<LuCircleHelp size={20} />} title="DAOs arrive with chain v8.3.0" data-testid="daos-unsupported">
          The chain does not run DAOs yet. They will show here as soon as it upgrades.
        </StateBox>
      ) : (
        <>
          <FilterPills label="Filter by type" options={FILTERS} value={type} onChange={setType} />
          {!list.data ? (
            list.error ? (
              <StateBox
                tone="error"
                icon={<LuTriangleAlert size={20} />}
                title="The DAOs could not be loaded"
                actions={
                  <Button size="sm" colorPalette="blue" onClick={() => void list.refetch()}>
                    Try again
                  </Button>
                }
                data-testid="daos-error"
              >
                The explorer did not answer. Try again in a moment.
              </StateBox>
            ) : (
              <TableSkeleton rows={6} columns={4} />
            )
          ) : items.length === 0 ? (
            <StateBox icon={<LuCircleHelp size={20} />} title={type ? "No DAO of this type yet" : "No DAO yet"} data-testid="daos-empty">
              <ExternalLink href={daoAppLinks.create}>Create one in the DAO app</ExternalLink>
            </StateBox>
          ) : (
            <ListPanel>
              <ListHead templateColumns={COLUMNS} aria-hidden>
                <Text>DAO</Text>
                <Text hideBelow="c2">Type</Text>
                <Text hideBelow="c2">Admin</Text>
                <Text hideBelow="c2">Created</Text>
              </ListHead>
              <Box role="list" aria-label="DAOs">
                {items.map((d) => (
                  <DaoRow key={d.id} d={d} />
                ))}
              </Box>
            </ListPanel>
          )}
          {list.isFetchingNextPage && (
            <Box mt="3">
              <TableSkeleton rows={3} columns={4} />
            </Box>
          )}
          {list.hasNextPage && !list.isFetchingNextPage && (
            <Box mt="3">
              <Button variant="outline" size="sm" onClick={() => void list.fetchNextPage()}>
                Show more
              </Button>
            </Box>
          )}
        </>
      )}
    </Box>
  );
}
