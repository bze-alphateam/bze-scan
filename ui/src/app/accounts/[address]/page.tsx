import type { Metadata } from "next";
import { notFound } from "next/navigation";

import type { Account } from "@/api/types";
import { AccountPage } from "@/components/accounts/account-page";
import { serverApiUrl } from "@/lib/env";
import { accountTitle, parsePage } from "@/render/account";
import { parseCategory } from "@/render/activity";
import { shareMeta } from "@/lib/page-meta";

// A `bze1…` account; contracts and interchain accounts are longer than 38.
const ADDRESS = /^bze1[02-9ac-hj-np-z]{38,}$/;

interface Props {
  params: Promise<{ address: string }>;
  searchParams: Promise<{ tab?: string | string[]; category?: string | string[]; page?: string | string[] }>;
}

/** The account, or null for any failure, read on the server for the title. */
async function fetchAccount(address: string): Promise<Account | null> {
  try {
    const res = await fetch(`${serverApiUrl()}/v1/accounts/${address}`, {
      cache: "no-store",
      headers: { Accept: "application/json" },
    });
    return res.ok ? ((await res.json()) as Account) : null;
  } catch {
    return null;
  }
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { address } = await params;
  if (!ADDRESS.test(address)) return { title: "Not found" };
  const a = await fetchAccount(address);
  const title = accountTitle(address, a?.label);
  const description = "A BZE account: its balances, tokens, stake and activity.";
  return { title, description, ...shareMeta(title, description) };
}

export default async function AccountRoute({ params, searchParams }: Props) {
  const { address } = await params;
  if (!ADDRESS.test(address)) notFound();
  const { tab, category, page } = await searchParams;
  return (
    <AccountPage
      key={address}
      address={address}
      initialTab={Array.isArray(tab) ? tab[0] : tab}
      initialCategory={parseCategory(category)}
      initialPage={parsePage(page)}
    />
  );
}
