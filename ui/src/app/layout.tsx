import { Container, Flex } from "@chakra-ui/react";
import type { Metadata, Viewport } from "next";
import { IBM_Plex_Mono, IBM_Plex_Sans } from "next/font/google";
import type { ReactNode } from "react";

import { Footer } from "@/components/layout/footer";
import { Header } from "@/components/layout/header";
import { NetworkStrip } from "@/components/layout/network-strip";
import { env } from "@/lib/env";

import { Providers } from "./providers";

const plexSans = IBM_Plex_Sans({
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
  display: "swap",
  variable: "--font-plex-sans",
});

const plexMono = IBM_Plex_Mono({
  subsets: ["latin"],
  weight: ["400", "500", "600"],
  display: "swap",
  variable: "--font-plex-mono",
});

const description =
  "Explore the BeeZee (BZE) network: blocks, transactions, accounts, validators, tokens, proposals and chain parameters, in plain words.";

export const metadata: Metadata = {
  metadataBase: new URL(env.siteUrl),
  title: { default: "BZE Explorer", template: "%s · BZE Explorer" },
  description,
  icons: { icon: "/images/logo_320px.png" },
  openGraph: { type: "website", siteName: "BZE Explorer", url: "/", title: "BZE Explorer", description },
  twitter: { card: "summary", title: "BZE Explorer", description },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className={`${plexSans.variable} ${plexMono.variable}`} suppressHydrationWarning>
      <body suppressHydrationWarning>
        <Providers>
          <Flex direction="column" minH="100dvh">
            <NetworkStrip />
            <Header />
            {/* No vertical padding: each page head brings its own (32px title, 24px breadcrumb), the footer its 40px. */}
            <Container as="main" maxW="7xl" px={{ base: "4", md: "6" }} flex="1">
              {children}
            </Container>
            <Footer />
          </Flex>
        </Providers>
      </body>
    </html>
  );
}
