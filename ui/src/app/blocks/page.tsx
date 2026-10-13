import type { Metadata } from "next";

import { BlocksPage } from "@/components/blocks/blocks-page";
import { parseBlocksPlace } from "@/render/blocks";

const description = "The latest BZE blocks: who proposed them, their transactions, block time and size.";

export const metadata: Metadata = {
  title: "Blocks",
  description,
  openGraph: { title: "Blocks", description },
  twitter: { title: "Blocks", description },
};

interface Props {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

/** `/blocks` is the live list; `?top=H&page=p` a page pinned to the height H. */
export default async function BlocksRoute({ searchParams }: Props) {
  const place = parseBlocksPlace(await searchParams);
  return <BlocksPage top={place.top} page={place.page} />;
}
