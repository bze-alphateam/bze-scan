import { env } from "./env";

export interface NavLink {
  label: string;
  href: string;
}

/** The explorer's own sections, in header order. */
export const navLinks: NavLink[] = [
  { label: "Blocks", href: "/blocks" },
  { label: "Transactions", href: "/txs" },
  { label: "Validators", href: "/validators" },
  { label: "Tokens", href: "/tokens" },
  { label: "Proposals", href: "/proposals" },
  { label: "Parameters", href: "/parameters" },
];

export const externalLinks = {
  website: "https://getbze.com",
  staking: "https://staking.getbze.com",
  dex: "https://dex.getbze.com",
  burner: "https://burner.getbze.com",
  factory: "https://factory.getbze.com",
  communities: "https://communities.getbze.com",
  docs: "https://docs.getbze.com",
  pingPub: env.pingPubUrl,
};

/** The "Apps" menu: the other BZE apps. */
export const appLinks: NavLink[] = [
  { label: "Website", href: externalLinks.website },
  { label: "Staking", href: externalLinks.staking },
  { label: "DEX", href: externalLinks.dex },
  { label: "Burner", href: externalLinks.burner },
  { label: "Factory", href: externalLinks.factory },
  { label: "Communities", href: externalLinks.communities },
  { label: "Docs", href: externalLinks.docs },
];

/** `linktr.ee/jasonsopko` from `https://linktr.ee/jasonsopko`; null for anything that is not an http(s) URL. */
export function websiteLink(website: string | null): { href: string; text: string } | null {
  if (!website) return null;
  const raw = /^https?:\/\//i.test(website) ? website : `https://${website}`;
  try {
    const url = new URL(raw);
    if (url.protocol !== "https:" && url.protocol !== "http:") return null;
    const text = (url.host + url.pathname).replace(/^www\./, "").replace(/\/$/, "");
    return { href: url.toString(), text };
  } catch {
    return null;
  }
}
