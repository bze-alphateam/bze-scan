import type { Metadata } from "next";
import { notFound } from "next/navigation";

import type { Tx } from "@/api/types";
import { TxPage } from "@/components/tx/tx-page";
import { serverApiUrl } from "@/lib/env";
import { shortHash } from "@/lib/format";
import { createRenderContext } from "@/render/messages/registry";
import { shareMeta } from "@/lib/page-meta";

const HASH = /^[0-9a-fA-F]{64}$/;

interface Props {
  params: Promise<{ hash: string }>;
}

/** The transaction, or "pending" (a 404) or null (any other failure), read on the server for the title. */
async function fetchTx(hash: string): Promise<Tx | "pending" | null> {
  try {
    const res = await fetch(`${serverApiUrl()}/v1/txs/${hash}`, { cache: "no-store", headers: { Accept: "application/json" } });
    if (res.status === 404) return "pending";
    return res.ok ? ((await res.json()) as Tx) : null;
  } catch {
    return null;
  }
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { hash } = await params;
  if (!HASH.test(hash)) return { title: "Not found" };
  const name = `Transaction ${shortHash(hash)}`;
  const tx = await fetchTx(hash.toUpperCase());
  if (tx === null) return { title: name };
  if (tx === "pending") return { title: `${name} · Pending` };

  const title = `${name} · ${tx.success ? "Success" : "Failed"}`;
  const first = tx.messages[0];
  if (!first) return { title };
  const ctx = createRenderContext({ transfers: tx.transfers });
  const description = ctx.rendererFor(first.type_url).title(first, ctx);
  return { title, description, ...shareMeta(title, description) };
}

export default async function TransactionRoute({ params }: Props) {
  const { hash } = await params;
  if (!HASH.test(hash)) notFound();
  const upper = hash.toUpperCase();
  return <TxPage key={upper} hash={upper} />;
}
