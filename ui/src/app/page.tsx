import type { Metadata } from "next";

import { HomePage } from "@/components/home/home-page";

const title = "BZE Explorer — blocks, transactions and accounts of the BeeZee network, explained in plain terms";
const description = "Search any block, transaction, address or validator of the BeeZee network and see in plain words what happened on chain.";

export const metadata: Metadata = {
  title: { absolute: title },
  description,
  openGraph: { title, description },
  twitter: { title, description },
};

export default function HomeRoute() {
  return <HomePage />;
}
