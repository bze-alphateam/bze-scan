import type { Metadata } from "next";
import { notFound } from "next/navigation";

import type { ValidatorDetail } from "@/api/types";
import { ValidatorPage } from "@/components/validators/validator-page";
import { serverApiUrl } from "@/lib/env";
import { shortAddress } from "@/lib/format";

const OPERATOR = /^bzevaloper1[02-9ac-hj-np-z]{38,}$/;

interface Props {
  params: Promise<{ operator: string }>;
}

/** The validator, or null for any failure, read on the server for the title. */
async function fetchValidator(operator: string): Promise<ValidatorDetail | null> {
  try {
    const res = await fetch(`${serverApiUrl()}/v1/validators/${operator}`, {
      cache: "no-store",
      headers: { Accept: "application/json" },
    });
    return res.ok ? ((await res.json()) as ValidatorDetail) : null;
  } catch {
    return null;
  }
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { operator } = await params;
  if (!OPERATOR.test(operator)) return { title: "Not found" };
  const v = await fetchValidator(operator);
  const title = `${v?.moniker || shortAddress(operator)} · validator`;
  const description = v?.details || `A validator of the BZE chain: stake, commission, uptime and history.`;
  return { title, description, openGraph: { title, description }, twitter: { title, description } };
}

export default async function ValidatorRoute({ params }: Props) {
  const { operator } = await params;
  if (!OPERATOR.test(operator)) notFound();
  return <ValidatorPage key={operator} operator={operator} />;
}
