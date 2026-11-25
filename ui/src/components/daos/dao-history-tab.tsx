"use client";

import { Box, Button, Flex, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { LuTriangleAlert } from "react-icons/lu";

import { useDaoHistory } from "@/api/hooks";
import type { Dao, DaoEvent, DaoSummary } from "@/api/types";
import { ListPanel } from "@/components/ui/list";
import { TableSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { TimeAgo } from "@/components/ui/time-ago";
import { shortHash } from "@/lib/format";
import { historyLine } from "@/render/daos";

import { WhoChip } from "./parts";

function Line({ e, dao, daos }: { e: DaoEvent; dao: Dao; daos: Map<string, DaoSummary> }) {
  const parts = historyLine(e, dao);
  return (
    <Flex
      gap="3"
      justify="space-between"
      align="flex-start"
      wrap="wrap"
      px="4"
      py="11px"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      fontSize="13px"
      data-testid="dao-history-row"
    >
      <Flex gap="1" align="center" wrap="wrap" minW="0" flex="1" flexBasis="240px">
        {parts.map((p, i) =>
          "text" in p ? (
            <Text as="span" key={i}>
              {p.text}
            </Text>
          ) : (
            <WhoChip key={i} address={p.address} daos={daos} label={p.address === dao.creator ? dao.creator_label : undefined} />
          ),
        )}
      </Flex>
      <Flex gap="2" align="center" color="fg.muted" fontSize="12px">
        <TimeAgo time={e.time} />
        {e.tx_hash && (
          <Link asChild fontFamily="mono" color="accent.fg">
            <NextLink href={`/txs/${e.tx_hash}`} title={e.tx_hash}>
              {shortHash(e.tx_hash)}
            </NextLink>
          </Link>
        )}
      </Flex>
    </Flex>
  );
}

/** What changed in the DAO, newest first: its creation, admin changes, members and rules. */
export function DaoHistoryTab({ dao, daos }: { dao: Dao; daos: Map<string, DaoSummary> }) {
  const history = useDaoHistory(dao.id);
  if (!history.data) {
    return history.error ? (
      <StateBox
        tone="error"
        icon={<LuTriangleAlert size={20} />}
        title="The history could not be loaded"
        actions={
          <Button size="sm" colorPalette="blue" onClick={() => void history.refetch()}>
            Try again
          </Button>
        }
        data-testid="dao-history-error"
      >
        The explorer did not answer. Try again in a moment.
      </StateBox>
    ) : (
      <TableSkeleton rows={4} columns={2} />
    );
  }
  const items = history.data.pages.flatMap((p) => p.items);
  return (
    <Box>
      <ListPanel>
        {items.length === 0 ? (
          <Text px="4" py="6" color="fg.muted" fontSize="13px" data-testid="dao-history-empty">
            Nothing has changed in this DAO yet.
          </Text>
        ) : (
          <Box role="list" aria-label="History">
            {items.map((e) => (
              <Line key={`${e.height}-${e.tx_index}-${e.msg_index}-${e.seq}`} e={e} dao={dao} daos={daos} />
            ))}
          </Box>
        )}
      </ListPanel>
      {history.isFetchingNextPage && (
        <Box mt="3">
          <TableSkeleton rows={3} columns={2} />
        </Box>
      )}
      {history.hasNextPage && !history.isFetchingNextPage && (
        <Box mt="3">
          <Button variant="outline" size="sm" onClick={() => void history.fetchNextPage()}>
            Show older changes
          </Button>
        </Box>
      )}
    </Box>
  );
}

