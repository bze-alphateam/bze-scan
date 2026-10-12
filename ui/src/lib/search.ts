import type { SearchResult } from "@/api/types";

// The same shapes the detail routes accept.
const HEIGHT = /^\d+$/;
const HASH = /^[0-9a-f]{64}$/i;
const ACCOUNT = /^bze1[02-9ac-hj-np-z]{38,}$/;
const VALIDATOR = /^bzevaloper1[02-9ac-hj-np-z]{38,}$/;

/**
 * What a query is, decided without a request: an identifier goes straight to
 * its page (`go`), other text of two characters or more is searched for by
 * name (`search`), anything shorter is `none`. A full `bze1…` address is also
 * `account`: the box looks it up to say whether the explorer has seen it.
 */
export type QueryKind =
  | { kind: "go"; href: string; account?: string }
  | { kind: "search"; q: string }
  | { kind: "none" };

export function classifyQuery(raw: string): QueryKind {
  const q = raw.trim();
  if (HEIGHT.test(q)) {
    const height = Number(q);
    if (height > 0 && Number.isSafeInteger(height)) return { kind: "go", href: `/blocks/${height}` };
  }
  if (HASH.test(q)) return { kind: "go", href: `/txs/${q.toUpperCase()}` };
  // Bech32 is either all lower or all upper case.
  const lower = q.toLowerCase();
  if (q === lower || q === q.toUpperCase()) {
    if (ACCOUNT.test(lower)) return { kind: "go", href: `/accounts/${lower}`, account: lower };
    if (VALIDATOR.test(lower)) return { kind: "go", href: `/validators/${lower}` };
  }
  return q.length >= 2 ? { kind: "search", q } : { kind: "none" };
}

/** The page of a search result. */
export function resultHref(r: SearchResult): string {
  switch (r.type) {
    case "validator":
      return `/validators/${encodeURIComponent(r.id)}`;
    case "account":
      return `/accounts/${encodeURIComponent(r.id)}`;
    case "block":
      return `/blocks/${encodeURIComponent(r.id)}`;
    case "transaction":
      return `/txs/${encodeURIComponent(r.id)}`;
    default:
      return `/search?q=${encodeURIComponent(r.id)}`;
  }
}

/** Validators first, then accounts, then anything else, each in the API's order. */
export function orderResults(results: SearchResult[]): SearchResult[] {
  const rank = (r: SearchResult) => (r.type === "validator" ? 0 : r.type === "account" ? 1 : 2);
  return results.map((r, i) => [r, i] as const).sort(([a, i], [b, j]) => rank(a) - rank(b) || i - j).map(([r]) => r);
}
