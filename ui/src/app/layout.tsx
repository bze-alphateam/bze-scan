import { Container, Flex } from "@chakra-ui/react";
import type { Metadata, Viewport } from "next";
import { Inter } from "next/font/google";
import type { ReactNode } from "react";

import { Footer } from "@/components/layout/footer";
import { Header } from "@/components/layout/header";
import { env } from "@/lib/env";

import { Providers } from "./providers";

const inter = Inter({
  subsets: ["latin"],
  display: "swap",
  variable: "--font-inter",
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
    <html lang="en" className={inter.variable} suppressHydrationWarning>
      <body suppressHydrationWarning>
        <Providers>
          <Flex direction="column" minH="100dvh">
            <Header />
            <Container as="main" maxW="7xl" px={{ base: "4", md: "6" }} py={{ base: "6", md: "10" }} flex="1">
              {children}
            </Container>
            <Footer />
          </Flex>
        </Providers>
      </body>
    </html>
  );
}
