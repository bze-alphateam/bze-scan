"use client";

import { Box, Button, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useRouter } from "next/navigation";
import { LuSearch, LuTriangleAlert } from "react-icons/lu";

import type { SearchResult } from "@/api/types";
import { SearchBox } from "@/components/layout/search-box";
import { ListPanel } from "@/components/ui/list";
import { PageTitle } from "@/components/ui/page-head";
import { StateBox } from "@/components/ui/state-box";
import { formatCount, shortAddress } from "@/lib/format";
import { orderResults, resultHref } from "@/lib/search";

import { SearchResultRow } from "./search-result-row";

interface Props {
  q: string;
  /** The matches; null when the search could not be run. */
  results: SearchResult[] | null;
  /** A full address the explorer has not seen: its "Open account" row instead of the matches. */
  account?: string;
}

function ResultLink({ r, open = false }: { r: SearchResult; open?: boolean }) {
  return (
    <Link
      asChild
      display="flex"
      px="4"
      py="3"
      minH="56px"
      borderTopWidth="1px"
      borderColor="border.panel"
      color="fg"
      _first={{ borderTopWidth: "0" }}
      _hover={{ bg: "bg.hover", textDecoration: "none" }}
      role="listitem"
      data-testid="search-result"
    >
      <NextLink href={resultHref(r)}>
        <SearchResultRow r={r} open={open} size="8" />
      </NextLink>
    </Link>
  );
}

function Outcome({ q, results, account }: Props) {
  const router = useRouter();
  if (q.length < 2) {
    return (
      <StateBox icon={<LuSearch size={20} />} title="Type a little more" data-testid="search-state">
        Type a block height, a transaction hash, an address, or two letters or more of a name.
      </StateBox>
    );
  }
  if (results == null) {
    return (
      <StateBox
        tone="error"
        icon={<LuTriangleAlert size={20} />}
        title="The search could not be run"
        actions={
          <Button size="sm" colorPalette="blue" onClick={() => router.refresh()}>
            Try again
          </Button>
        }
        data-testid="search-state"
      >
        Try again in a moment.
      </StateBox>
    );
  }
  if (account) {
    return (
      <>
        <ListPanel role="list" aria-label="Search results">
          <ResultLink r={{ type: "account", id: account, label: "", indexed: false }} open />
        </ListPanel>
        <Text mt="3" maxW="880px" fontSize="12.5px" color="fg.muted">
          The address is valid but has no activity in the explorer&apos;s history. Its page still shows its live balances.
        </Text>
      </>
    );
  }
  if (results.length === 0) {
    return (
      <StateBox icon={<LuSearch size={20} />} title={`No validator or known account matches “${q}”`} data-testid="search-state">
        Search finds block heights, transaction hashes, addresses, validator names and the names of known accounts.
      </StateBox>
    );
  }
  return (
    <>
      <Text mb="2" fontSize="12px" color="fg.muted" data-testid="search-count">
        <Text as="span" fontFamily="mono">
          {formatCount(results.length)}
        </Text>{" "}
        {results.length === 1 ? "match" : "matches"} · validators first
      </Text>
      <ListPanel role="list" aria-label="Search results">
        {orderResults(results).map((r) => (
          <ResultLink key={`${r.type}:${r.id}`} r={r} />
        ))}
      </ListPanel>
    </>
  );
}

/** The search results as a page: for direct links and browsers without JavaScript. */
export function SearchResultsPage({ q, results, account }: Props) {
  return (
    <Box>
      <PageTitle title={q.length < 2 ? "Search" : `Results for “${account ? shortAddress(account) : q}”`} />
      <Box maxW="720px" mb="5">
        <SearchBox size="lg" />
      </Box>
      <Outcome q={q} results={results} account={account} />
    </Box>
  );
}
