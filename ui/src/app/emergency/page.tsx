import type { Metadata } from "next";

import { EmergencyPage } from "@/components/emergency/emergency-page";
import { shareMeta } from "@/lib/page-meta";

const description = "Whether any emergency measure is in force on BZE, what it means for you, and the history of the switches pulled and undone.";

export const metadata: Metadata = {
  title: "Emergency status",
  description,
  ...shareMeta("Emergency status", description),
};

export default function EmergencyRoute() {
  return <EmergencyPage />;
}
