"use client";

import { Box, Heading, Link, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";

import type { SearchResult } from "@/api/types";
import { SearchBox } from "@/components/layout/search-box";
import { orderResults, resultHref } from "@/lib/search";

import { SearchResultRow } from "./search-result-row";

interface Props {
  q: string;
  /** The matches; null when the search could not be run. */
  results: SearchResult[] | null;
}

/** The search results as a page: for direct links and browsers without JavaScript. */
export function SearchResultsPage({ q, results }: Props) {
  const short = q.length < 2;
  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Stack gap="3">
        <Heading as="h1" size={{ base: "2xl", md: "3xl" }}>
          {short ? "Search" : `Results for “${q}”`}
        </Heading>
        <Box maxW="2xl">
          <SearchBox />
        </Box>
      </Stack>
      {short ? (
        <Text color="fg.muted">Type a block height, a transaction hash, an address, or two letters or more of a name.</Text>
      ) : results == null ? (
        <Text color="fg.muted">The search could not be run. Try again in a moment.</Text>
      ) : results.length === 0 ? (
        <Text color="fg.muted">No validator or known account matches</Text>
      ) : (
        <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" role="list" aria-label="Search results">
          {orderResults(results).map((r) => (
            <Link
              key={`${r.type}:${r.id}`}
              asChild
              display="flex"
              px="4"
              py="3"
              minH="56px"
              borderTopWidth="1px"
              borderColor="border.panel"
              _first={{ borderTopWidth: "0" }}
              _hover={{ bg: "bg.muted", textDecoration: "none" }}
              role="listitem"
              data-testid="search-result"
            >
              <NextLink href={resultHref(r)}>
                <SearchResultRow r={r} />
              </NextLink>
            </Link>
          ))}
        </Box>
      )}
    </Stack>
  );
}
