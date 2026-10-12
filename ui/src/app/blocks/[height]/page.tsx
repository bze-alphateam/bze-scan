import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { BlockPage } from "@/components/blocks/block-page";
import { formatCount } from "@/lib/format";

interface Props {
  params: Promise<{ height: string }>;
}

/** A positive integer height, else null. */
function parseHeight(raw: string): number | null {
  if (!/^[1-9]\d{0,15}$/.test(raw)) return null;
  const height = Number(raw);
  return Number.isSafeInteger(height) ? height : null;
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const height = parseHeight((await params).height);
  if (height == null) return { title: "Not found" };
  const title = `Block ${formatCount(height)}`;
  const description = `Block ${formatCount(height)} on BZE: its transactions in plain words and where the funds went.`;
  return { title, description, openGraph: { title, description }, twitter: { title, description } };
}

export default async function BlockRoute({ params }: Props) {
  const height = parseHeight((await params).height);
  if (height == null) notFound();
  return <BlockPage key={height} height={height} />;
}
