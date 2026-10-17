import type { MetadataRoute } from "next";

import { SITE_NAME } from "@/lib/page-meta";

/** The web app manifest: name, colours and the install icons from the BeeZee app identity. */
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: SITE_NAME,
    short_name: "Explorer",
    description: "Blocks, transactions and validators of the BeeZee network, in plain words.",
    start_url: "/",
    display: "standalone",
    theme_color: "#1694D2",
    background_color: "#090D11",
    icons: [
      { src: "/images/explorer/icon-192.png", sizes: "192x192", type: "image/png" },
      { src: "/images/explorer/icon-512.png", sizes: "512x512", type: "image/png" },
      { src: "/images/explorer/maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
    ],
  };
}
