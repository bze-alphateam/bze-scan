import type { Metadata } from "next";
import { notFound } from "next/navigation";

import type { DaoProposal } from "@/api/types";
import { DaoProposalPage } from "@/components/daos/dao-proposal-page";
import { shareMeta } from "@/lib/page-meta";
import { serverJson } from "@/lib/server-json";
import { parseDaoId } from "@/render/daos";
import { statusInfo } from "@/render/dao-proposals";

interface Props {
  params: Promise<{ id: string; proposalId: string }>;
  searchParams: Promise<{ tab?: string | string[] }>;
}

/** The proposal, or null for any failure, read on the server for the title. */
async function fetchProposal(id: number): Promise<DaoProposal | null> {
  const res = await serverJson<DaoProposal>(`/v1/dao-proposals/${id}`);
  return res.ok ? res.data : null;
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { id: rawDao, proposalId } = await params;
  const daoId = parseDaoId(rawDao);
  const id = parseDaoId(proposalId);
  if (daoId == null || id == null) return { title: "Not found" };
  const p = await fetchProposal(id);
  const title = p?.title ? `#${id} ${p.title} · ${statusInfo(p.status).label}` : `DAO proposal #${id}`;
  const description = `A proposal of DAO #${daoId} on BZE: what it does, the votes and the result.`;
  return { title, description, ...shareMeta(title, description) };
}

export default async function DaoProposalRoute({ params, searchParams }: Props) {
  const { id: rawDao, proposalId } = await params;
  const daoId = parseDaoId(rawDao);
  const id = parseDaoId(proposalId);
  if (daoId == null || id == null) notFound();
  const { tab } = await searchParams;
  return <DaoProposalPage key={id} id={id} daoId={daoId} initialTab={Array.isArray(tab) ? tab[0] : tab} />;
}
