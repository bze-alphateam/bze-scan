import type { Metadata } from "next";

import { ProposalsPage } from "@/components/proposals/proposals-page";

const description = "Every governance proposal on BZE: the live vote, what each proposal changes and how it was decided.";

export const metadata: Metadata = {
  title: "Proposals",
  description,
  openGraph: { title: "Proposals", description },
  twitter: { title: "Proposals", description },
};

export default function ProposalsRoute() {
  return <ProposalsPage />;
}
