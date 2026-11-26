import type { Metadata } from "next";

import { DaosPage } from "@/components/daos/daos-page";
import { shareMeta } from "@/lib/page-meta";

const description = "Every DAO on BZE: who runs it, who votes in it and what its treasury holds.";

export const metadata: Metadata = {
  title: "DAOs",
  description,
  ...shareMeta("DAOs", description),
};

export default function DaosRoute() {
  return <DaosPage />;
}
