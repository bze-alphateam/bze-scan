import { Container, Flex } from "@chakra-ui/react";
import type { Metadata, Viewport } from "next";
import { IBM_Plex_Mono, IBM_Plex_Sans } from "next/font/google";
import type { ReactNode } from "react";

import { Footer } from "@/components/layout/footer";
import { Header } from "@/components/layout/header";
import { NetworkStrip } from "@/components/layout/network-strip";
import { siteMetadata } from "@/lib/page-meta";

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

export const metadata: Metadata = siteMetadata;

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
