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
    <Box as="footer" bg="bg.panel" borderTopWidth="1px" borderColor="border.panel" mt="10">
      <Container maxW="7xl" px={{ base: "4", md: "6" }}>
        <Flex wrap="wrap" columnGap="6" rowGap="3" align="center" minH="64px" py="3" fontSize="12px">
          <Text color="fg.muted">
            BZE Explorer · chain{" "}
            <Text as="span" fontFamily="mono">
              {env.chainId}
            </Text>
          </Text>
          <HStack as="nav" aria-label="Footer" columnGap="18px" rowGap="1" flexWrap="wrap">
            {FOOTER_LINKS.map((l) =>
              l.internal ? (
                <Link key={l.label} asChild minH="32px" color="fg.muted" _hover={{ color: "fg" }}>
                  <NextLink href={l.href}>{l.label}</NextLink>
                </Link>
              ) : (
                <Link key={l.label} href={l.href} minH="32px" color="fg.muted" _hover={{ color: "fg" }}>
                  {l.label}
                </Link>
              ),
            )}
          </HStack>
          <Box ms="auto">
            <StatusLight />
          </Box>
        </Flex>
      </Container>
    </Box>
  );
}
