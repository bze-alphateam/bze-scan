import type { Metadata } from "next";

import { ProposalsPage } from "@/components/proposals/proposals-page";
import { shareMeta } from "@/lib/page-meta";

const description = "Every governance proposal on BZE: the live vote, what each proposal changes and how it was decided.";

export const metadata: Metadata = {
  title: "Proposals",
  description,
  ...shareMeta("Proposals", description),
};

export default function ProposalsRoute() {
  return <ProposalsPage />;
}
