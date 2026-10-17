import type { Metadata } from "next";
import { notFound } from "next/navigation";

import type { Token } from "@/api/types";
import { TokenPage } from "@/components/tokens/token-page";
import { serverApiUrl } from "@/lib/env";
import { denomFromSegments, tokenSymbol } from "@/render/tokens";
import { shareMeta } from "@/lib/page-meta";

interface Props {
  // A catch-all: `/tokens/factory%2Fbze1…%2Fuhoney` and `/tokens/factory/bze1…/uhoney` are the same page.
  params: Promise<{ denom: string[] }>;
}

/** The token, or null for any failure, read on the server for the title. */
async function fetchToken(denom: string): Promise<Token | null> {
  try {
    const res = await fetch(`${serverApiUrl()}/v1/tokens/${encodeURIComponent(denom)}`, {
      cache: "no-store",
      headers: { Accept: "application/json" },
    });
    return res.ok ? ((await res.json()) as Token) : null;
  } catch {
    return null;
  }
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const denom = denomFromSegments((await params).denom);
  if (!denom) return { title: "Not found" };
  const t = await fetchToken(denom);
  const title = t ? [tokenSymbol(t), t.name].filter(Boolean).join(" · ") : "Token";
  const description = t?.description || "A token on BZE: its supply, holders, price and recent activity.";
  return { title, description, ...shareMeta(title, description) };
}

export default async function TokenRoute({ params }: Props) {
  const denom = denomFromSegments((await params).denom);
  if (!denom) notFound();
  return <TokenPage key={denom} denom={denom} />;
}
