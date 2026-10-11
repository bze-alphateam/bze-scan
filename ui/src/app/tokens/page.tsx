import type { Metadata } from "next";

import { TokensPage } from "@/components/tokens/tokens-page";

const description = "Every token on BZE: where it comes from, its supply, holders and DEX price.";

export const metadata: Metadata = {
  title: "Tokens",
  description,
  openGraph: { title: "Tokens", description },
  twitter: { title: "Tokens", description },
};

export default function TokensRoute() {
  return <TokensPage />;
}
