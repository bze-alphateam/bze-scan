import type { Metadata } from "next";

import { env } from "@/lib/env";

export const SITE_NAME = "BZE Explorer";

const SITE_DESCRIPTION =
  "Explore the BeeZee (BZE) network: blocks, transactions, accounts, validators, tokens, proposals and chain parameters, in plain words.";

/** The share image every page carries (`public/images/og-image.png`, from the BeeZee app identity). */
export const OG_IMAGE = {
  url: "/images/og-image.png",
  width: 1200,
  height: 630,
  alt: "BZE Explorer: blocks, transactions and validators, in plain words.",
};

/**
 * The `openGraph` and `twitter` metadata of a page. Next.js replaces the
 * layout's whole object when a page sets its own, so every page builds both
 * here: the image, site name, type and large card come with the title.
 */
export function shareMeta(title: string, description: string): Pick<Metadata, "openGraph" | "twitter"> {
  return {
    openGraph: { type: "website", siteName: SITE_NAME, title, description, images: [OG_IMAGE] },
    twitter: { card: "summary_large_image", title, description, images: [OG_IMAGE] },
  };
}

/** The root layout's metadata: title template, icons and the site-wide share tags. */
export const siteMetadata: Metadata = {
  metadataBase: new URL(env.siteUrl),
  title: { default: SITE_NAME, template: `%s · ${SITE_NAME}` },
  description: SITE_DESCRIPTION,
  icons: {
    icon: [
      { url: "/images/explorer/favicon-32.png", sizes: "32x32", type: "image/png" },
      { url: "/images/explorer/favicon-16.png", sizes: "16x16", type: "image/png" },
    ],
    apple: "/images/explorer/apple-touch-icon.png",
  },
  ...shareMeta(SITE_NAME, SITE_DESCRIPTION),
};
