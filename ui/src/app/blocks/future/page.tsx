import type { Metadata } from "next";

import { FutureBlockPage } from "@/components/blocks/future-block";

const description = "When a future BZE block will be produced, or which block the chain will be at on a date: an estimate from the average block time.";

export const metadata: Metadata = {
  title: "Future block",
  description,
  openGraph: { title: "Future block", description },
  twitter: { title: "Future block", description },
};

interface Props {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

const one = (v: string | string[] | undefined) => (Array.isArray(v) ? v[0] : v);

/** `/blocks/future`: `?height=<n>` opens By block height with it, `?date=YYYY-MM-DDTHH:MM` (local time) By date. */
export default async function FutureBlockRoute({ searchParams }: Props) {
  const params = await searchParams;
  const height = one(params.height);
  return <FutureBlockPage height={height && /^\d+$/.test(height) ? Number(height).toLocaleString("en-US") : undefined} date={one(params.date)} />;
}
