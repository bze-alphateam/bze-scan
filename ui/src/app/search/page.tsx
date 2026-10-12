import type { Metadata } from "next";
import { redirect } from "next/navigation";

import type { SearchResult } from "@/api/types";
import { SearchResultsPage } from "@/components/search/search-results-page";
import { serverApiUrl } from "@/lib/env";
import { classifyQuery } from "@/lib/search";

interface Props {
  searchParams: Promise<{ q?: string | string[] }>;
}

const queryOf = async (searchParams: Props["searchParams"]) => {
  const { q } = await searchParams;
  return (Array.isArray(q) ? q[0] : q)?.trim() ?? "";
};

/** The search route's matches, read on the server so the page works without JavaScript; null on any failure. */
async function fetchResults(q: string): Promise<SearchResult[] | null> {
  try {
    const res = await fetch(`${serverApiUrl()}/v1/search?q=${encodeURIComponent(q)}`, {
      cache: "no-store",
      headers: { Accept: "application/json" },
    });
    return res.ok ? ((await res.json()) as { results: SearchResult[] }).results : null;
  } catch {
    return null;
  }
}

export async function generateMetadata({ searchParams }: Props): Promise<Metadata> {
  const q = await queryOf(searchParams);
  return { title: q ? `Search “${q}”` : "Search", robots: { index: false } };
}

/** `/search?q=`: an identifier opens its page, other text lists the validators and known accounts it matches. */
export default async function SearchRoute({ searchParams }: Props) {
  const q = await queryOf(searchParams);
  const kind = classifyQuery(q);
  if (kind.kind === "go") redirect(kind.href);
  const results = kind.kind === "search" ? await fetchResults(kind.q) : [];
  return <SearchResultsPage q={q} results={results} />;
}
