import type { Metadata } from "next";

import { IbcPage } from "@/components/ibc/ibc-page";
import { shareMeta } from "@/lib/page-meta";

const description = "BZE's IBC channels and the rate limits on each: how much of every token may still cross today.";

export const metadata: Metadata = {
  title: "Cross-chain",
  description,
  ...shareMeta("Cross-chain", description),
};

export default function IbcRoute() {
  return <IbcPage />;
}
