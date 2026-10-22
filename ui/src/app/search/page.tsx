import type { Metadata } from "next";
import { redirect } from "next/navigation";

import type { SearchResult } from "@/api/types";
import { SearchResultsPage } from "@/components/search/search-results-page";
import { classifyQuery } from "@/lib/search";
import { serverJson } from "@/lib/server-json";

interface Props {
  searchParams: Promise<{ q?: string | string[] }>;
}

const queryOf = async (searchParams: Props["searchParams"]) => {
  const { q } = await searchParams;
  return (Array.isArray(q) ? q[0] : q)?.trim() ?? "";
};

/** The search route's matches, read on the server so the page works without JavaScript; null on any failure. */
async function fetchResults(q: string): Promise<SearchResult[] | null> {
  const res = await serverJson<{ results: SearchResult[] }>(`/v1/search?q=${encodeURIComponent(q)}`);
  return res.ok ? res.data.results : null;
}

export async function generateMetadata({ searchParams }: Props): Promise<Metadata> {
  const q = await queryOf(searchParams);
  return { title: q ? `Search “${q}”` : "Search", robots: { index: false } };
}

/**
 * `/search?q=`: an identifier opens its page, other text lists the validators and known accounts it matches.
 * A full address the explorer has not seen stays here as its "Open account" row, saying so.
 */
export default async function SearchRoute({ searchParams }: Props) {
  const q = await queryOf(searchParams);
  const kind = classifyQuery(q);
  if (kind.kind === "go") {
    const { account } = kind;
    const unseen = account != null && (await fetchResults(account))?.find((r) => r.id === account)?.indexed === false;
    if (unseen) return <SearchResultsPage q={q} results={[]} account={account} />;
    redirect(kind.href);
  }
  const results = kind.kind === "search" ? await fetchResults(kind.q) : [];
  return <SearchResultsPage q={q} results={results} />;
}
