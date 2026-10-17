import type { Metadata } from "next";

import { TokensPage } from "@/components/tokens/tokens-page";
import { shareMeta } from "@/lib/page-meta";

const description = "Every token on BZE: where it comes from, its supply, holders and DEX price.";

export const metadata: Metadata = {
  title: "Tokens",
  description,
  ...shareMeta("Tokens", description),
};

export default function TokensRoute() {
  return <TokensPage />;
}
