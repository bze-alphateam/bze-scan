import type { Metadata } from "next";
import { notFound, redirect } from "next/navigation";

import type { DaoProposal } from "@/api/types";
import { DaoProposalPage } from "@/components/daos/dao-proposal-page";
import { serverJson } from "@/lib/server-json";
import { parseDaoId } from "@/render/daos";
import { daoProposalHref } from "@/render/dao-proposals";

interface Props {
  params: Promise<{ id: string }>;
}

export const metadata: Metadata = { title: "DAO proposal" };

/**
 * `/dao-proposals/[id]`: sends the reader to the proposal under its DAO. A
 * proposal whose DAO the explorer has not indexed has no such address, so it
 * is shown here; an unknown one is a 404.
 */
export default async function DaoProposalRedirect({ params }: Props) {
  const id = parseDaoId((await params).id);
  if (id == null) notFound();
  const res = await serverJson<DaoProposal>(`/v1/dao-proposals/${id}`);
  if (res.ok && res.data.dao_id != null) redirect(daoProposalHref(res.data.dao_id, id));
  if (!res.ok && res.status === 404) notFound();
  return <DaoProposalPage key={id} id={id} />;
}
