import type { MetadataRoute } from "next";

import { env } from "@/lib/env";

/** The mainnet chain id: only its explorer is worth indexing. */
const MAINNET = "beezee-1";

/**
 * Crawlers: the overview pages (home, tokens, validators, proposals, accounts)
 * are open; single blocks and transactions, and the paged lists of both, are
 * not. They carry no search value and were most of the server's page renders.
 * A testnet explorer is closed entirely.
 */
export default function robots(): MetadataRoute.Robots {
  if (env.chainId !== MAINNET) {
    return { rules: { userAgent: "*", disallow: "/" } };
  }
  return {
    rules: {
      userAgent: "*",
      allow: "/",
      disallow: ["/blocks/", "/blocks?", "/txs/", "/txs?", "/search"],
    },
  };
}
