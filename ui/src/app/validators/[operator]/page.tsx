import type { Metadata } from "next";
import { notFound } from "next/navigation";

import type { ValidatorDetail } from "@/api/types";
import { ValidatorPage } from "@/components/validators/validator-page";
import { shortAddress } from "@/lib/format";
import { shareMeta } from "@/lib/page-meta";
import { serverJson } from "@/lib/server-json";

const OPERATOR = /^bzevaloper1[02-9ac-hj-np-z]{38,}$/;

interface Props {
  params: Promise<{ operator: string }>;
}

/** The validator, or null for any failure, read on the server for the title. */
async function fetchValidator(operator: string): Promise<ValidatorDetail | null> {
  const res = await serverJson<ValidatorDetail>(`/v1/validators/${operator}`);
  return res.ok ? res.data : null;
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { operator } = await params;
  if (!OPERATOR.test(operator)) return { title: "Not found" };
  const v = await fetchValidator(operator);
  const title = `${v?.moniker || shortAddress(operator)} · validator`;
  const description = v?.details || `A validator of the BZE chain: stake, commission, uptime and history.`;
  return { title, description, ...shareMeta(title, description) };
}

export default async function ValidatorRoute({ params }: Props) {
  const { operator } = await params;
  if (!OPERATOR.test(operator)) notFound();
  return <ValidatorPage key={operator} operator={operator} />;
}
