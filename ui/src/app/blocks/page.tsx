import type { Metadata } from "next";

import { BlocksPage } from "@/components/blocks/blocks-page";

const description = "The latest BZE blocks: who proposed them, their transactions, block time and size.";

export const metadata: Metadata = {
  title: "Blocks",
  description,
  openGraph: { title: "Blocks", description },
  twitter: { title: "Blocks", description },
};

export default function BlocksRoute() {
  return <BlocksPage />;
}
