import type { Metadata } from "next";
import { notFound } from "next/navigation";

import type { Proposal } from "@/api/types";
import { ProposalPage } from "@/components/proposals/proposal-page";
import { serverApiUrl } from "@/lib/env";
import { excerpt, titleStatus } from "@/render/proposals";

interface Props {
  params: Promise<{ id: string }>;
}

/** A positive integer id, else null. */
function parseId(raw: string): number | null {
  if (!/^[1-9]\d{0,15}$/.test(raw)) return null;
  const id = Number(raw);
  return Number.isSafeInteger(id) ? id : null;
}

/** The proposal, or null for any failure, read on the server for the title. */
async function fetchProposal(id: number): Promise<Proposal | null> {
  try {
    const res = await fetch(`${serverApiUrl()}/v1/proposals/${id}`, { cache: "no-store", headers: { Accept: "application/json" } });
    return res.ok ? ((await res.json()) as Proposal) : null;
  } catch {
    return null;
  }
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const id = parseId((await params).id);
  if (id == null) return { title: "Not found" };
  const p = await fetchProposal(id);
  const title = p ? `#${id} ${p.title} · ${titleStatus(p, new Date())}` : `Proposal #${id}`;
  const description = (p?.summary && excerpt(p.summary, 200)) || "A governance proposal on BZE: what it changes, the votes and the result.";
  return { title, description, openGraph: { title, description }, twitter: { title, description } };
}

export default async function ProposalRoute({ params }: Props) {
  const id = parseId((await params).id);
  if (id == null) notFound();
  return <ProposalPage key={id} id={id} />;
}
