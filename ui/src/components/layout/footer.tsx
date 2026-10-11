import { Box, Container, Flex, HStack, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";

import { env } from "@/lib/env";
import { externalLinks } from "@/lib/links";

import { StatusLight } from "./status-light";

// Parameters is the explorer's own page; the others leave the site.
const FOOTER_LINKS = [
  { label: "Website", href: externalLinks.website },
  { label: "Docs", href: externalLinks.docs },
  { label: "DEX", href: externalLinks.dex },
  { label: "Staking", href: externalLinks.staking },
  { label: "Parameters", href: "/parameters", internal: true },
  { label: "Ping.pub explorer", href: externalLinks.pingPub },
];

export function Footer() {
  return (
    <Box as="footer" bg="bg.panel" borderTopWidth="1px" borderColor="border.panel" mt="auto">
      <Container maxW="7xl" px={{ base: "4", md: "6" }} py="4">
        <Flex direction={{ base: "column", md: "row" }} gap="3" align={{ base: "flex-start", md: "center" }}>
          <Text fontSize="sm" color="fg.muted">
            BZE Explorer · chain {env.chainId}
          </Text>
          <HStack as="nav" aria-label="Footer" gap="0" flexWrap="wrap" fontSize="sm">
            {FOOTER_LINKS.map((l) =>
              l.internal ? (
                <Link key={l.label} asChild px="2" minH="44px" color="fg.muted">
                  <NextLink href={l.href}>{l.label}</NextLink>
                </Link>
              ) : (
                <Link key={l.label} href={l.href} px="2" minH="44px" color="fg.muted">
                  {l.label}
                </Link>
              ),
            )}
          </HStack>
          <Box ms={{ md: "auto" }}>
            <StatusLight />
          </Box>
        </Flex>
      </Container>
    </Box>
  );
}
