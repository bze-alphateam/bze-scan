import type { Metadata } from "next";

import { ParametersPage } from "@/components/parameters/parameters-page";
import { shareMeta } from "@/lib/page-meta";

const description = "The rules BZE runs by, read live from the chain: fees, staking, inflation, penalties, governance, the DEX and more.";

interface Props {
  searchParams: Promise<{ group?: string | string[] }>;
}

export function generateMetadata(): Metadata {
  return {
    title: "Chain parameters",
    description,
    ...shareMeta("Chain parameters", description),
  };
}

export default async function ParametersRoute({ searchParams }: Props) {
  const { group } = await searchParams;
  return <ParametersPage group={typeof group === "string" ? group : null} />;
}
