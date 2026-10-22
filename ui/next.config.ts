import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  // nginx and Cloudflare compress in front of us; gzip in Node only spends
  // memory outside the JS heap on every page.
  compress: false,
  // A self-contained server for the image (docker/ui.Dockerfile).
  output: "standalone",
  experimental: {
    optimizePackageImports: ["@chakra-ui/react"],
  },
};

export default nextConfig;
