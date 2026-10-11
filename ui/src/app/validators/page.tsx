import type { Metadata } from "next";

import { ValidatorsPage } from "@/components/validators/validators-page";

const description = "The validators that run the BZE chain: stake, commission and uptime of each.";

export const metadata: Metadata = {
  title: "Validators",
  description,
  openGraph: { title: "Validators", description },
  twitter: { title: "Validators", description },
};

export default function ValidatorsRoute() {
  return <ValidatorsPage />;
}
