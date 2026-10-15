// Build-time settings (`ui/.env.dist` lists every key). `NEXT_PUBLIC_*`
// values are inlined into the browser bundle by `next build`, so each is read
// by its literal name.

const trimSlash = (url: string) => url.replace(/\/+$/, "");

export const env = {
  /** The API as the browser reaches it, without `/v1`. */
  apiUrl: trimSlash(process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api"),
  chainId: process.env.NEXT_PUBLIC_CHAIN_ID || "beezee-1",
  networkLabel: process.env.NEXT_PUBLIC_NETWORK_LABEL || "Mainnet",
  pingPubUrl: trimSlash(process.env.NEXT_PUBLIC_PINGPUB_URL || "https://explorer.getbze.com/beezee"),
  siteUrl: trimSlash(process.env.NEXT_PUBLIC_SITE_URL || "https://explorer.getbze.com"),
};

/** The API as the Next.js server reaches it: `API_URL`, else the public URL. */
export function serverApiUrl(): string {
  return trimSlash(process.env.API_URL || env.apiUrl);
}
