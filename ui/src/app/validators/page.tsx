import type { Metadata } from "next";

import { ValidatorsPage } from "@/components/validators/validators-page";
import { shareMeta } from "@/lib/page-meta";

const description = "The validators that run the BZE chain: stake, commission and uptime of each.";

export const metadata: Metadata = {
  title: "Validators",
  description,
  ...shareMeta("Validators", description),
};

export default function ValidatorsRoute() {
  return <ValidatorsPage />;
}
