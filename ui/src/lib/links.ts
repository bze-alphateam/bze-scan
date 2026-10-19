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
  ...env.apps,
  docs: "https://docs.getbze.com",
  pingPub: env.pingPubUrl,
};

/**
 * The staking app's form for one validator: `?stake=<operator>` opens its
 * delegate form (BFE-172; until it ships the staking app ignores the
 * parameter and shows its page).
 */
export function stakeHref(operator: string): string {
  return `${externalLinks.staking}/?stake=${encodeURIComponent(operator)}`;
}

export type AppKey = keyof typeof env.apps;

/** An app icon for a light and a dark background ("on-dark"). */
export interface IconPair {
  light: string;
  dark: string;
}

export interface AppIdentity {
  /** The full icon, for 48 px and up. */
  icon: IconPair;
  /** The small cut (thicker ring, no inner details), for 32 px and below. */
  iconSmall: IconPair;
  /** Text and accents: the ink on light, the light end on dark. */
  accent: IconPair;
}

function identity(app: AppKey, ink: string, lightEnd: string): AppIdentity {
  const dir = `/images/apps/${app}`;
  return {
    icon: { light: `${dir}/icon.svg`, dark: `${dir}/icon-on-dark.svg` },
    iconSmall: { light: `${dir}/icon-small.svg`, dark: `${dir}/icon-small-on-dark.svg` },
    accent: { light: ink, dark: lightEnd },
  };
}

/**
 * Each app's icon and colour (BFE-214). The icons are copied unchanged from
 * Official-BZEdge-Graphics, beezee/apps, whose README holds the rules.
 */
export const appIdentity: Record<AppKey, AppIdentity> = {
  dex: identity("dex", "#0E74AD", "#12C1E1"),
  staking: identity("staking", "#8E44AD", "#B57EDC"),
  burner: identity("burner", "#C24A00", "#FF8A1F"),
  factory: identity("factory", "#9A6A00", "#FFC933"),
  communities: identity("communities", "#1A7A43", "#3DD07A"),
};

/** The mark in front of an app link: an app's icon, the BZE round logo or the docs book. */
export type AppLinkIcon = AppKey | "website" | "docs";

export interface AppLink extends NavLink {
  icon: AppLinkIcon;
}

/** The "Apps" menu: the other BZE apps. */
export const appLinks: AppLink[] = [
  { label: "Website", href: externalLinks.website, icon: "website" },
  { label: "Staking", href: externalLinks.staking, icon: "staking" },
  { label: "DEX", href: externalLinks.dex, icon: "dex" },
  { label: "Burner", href: externalLinks.burner, icon: "burner" },
  { label: "Factory", href: externalLinks.factory, icon: "factory" },
  { label: "Communities", href: externalLinks.communities, icon: "communities" },
  { label: "Docs", href: externalLinks.docs, icon: "docs" },
];

/** The app's accent for a link to it; null for Website and Docs, which keep the explorer's accent. */
export function appAccent(icon: AppLinkIcon): IconPair | null {
  return icon === "website" || icon === "docs" ? null : appIdentity[icon].accent;
}

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

/**
 * Where people vote on proposal `id`: the explorer is read-only, so the
 * proposal page lists these as data. The BZE Staking app's proposal page
 * votes with the staked BZE (Keplr, Leap and the other wallets it supports).
 */
export function stakingVoteHref(id: number): string {
  return `${externalLinks.staking}/governance/${id}`;
}

/** A proposal on Ping.pub. */
export function pingPubProposal(id: number): string {
  return `${env.pingPubUrl}/gov/${id}`;
}
