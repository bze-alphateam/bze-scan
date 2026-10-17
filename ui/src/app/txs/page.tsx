import type { Metadata } from "next";

import { TxsPage } from "@/components/txs/txs-page";
import { shareMeta } from "@/lib/page-meta";

const description = "The latest BZE transactions, each explained in one line: what happened, who did it, the fee.";

export const metadata: Metadata = {
  title: "Transactions",
  description,
  ...shareMeta("Transactions", description),
};

export default function TxsRoute() {
  return <TxsPage />;
}
