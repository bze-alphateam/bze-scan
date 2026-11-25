import type { Metadata } from "next";
import { notFound } from "next/navigation";

import type { Dao } from "@/api/types";
import { DaoPage } from "@/components/daos/dao-page";
import { shareMeta } from "@/lib/page-meta";
import { serverJson } from "@/lib/server-json";
import { parseDaoId } from "@/render/daos";

interface Props {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ tab?: string | string[] }>;
}

/** The DAO, or null for any failure, read on the server for the title. */
async function fetchDao(id: number): Promise<Dao | null> {
  const res = await serverJson<Dao>(`/v1/daos/${id}`);
  return res.ok ? res.data : null;
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const id = parseDaoId((await params).id);
  if (id == null) return { title: "Not found" };
  const d = await fetchDao(id);
  const title = d?.name ? `DAO #${id} · ${d.name}` : `DAO #${id}`;
  const description = d?.live.description || "A DAO on BZE: who runs it, who votes in it, its treasury, rules and history.";
  return { title, description, ...shareMeta(title, description) };
}

export default async function DaoRoute({ params, searchParams }: Props) {
  const id = parseDaoId((await params).id);
  if (id == null) notFound();
  const { tab } = await searchParams;
  return <DaoPage key={id} id={id} initialTab={Array.isArray(tab) ? tab[0] : tab} />;
}
