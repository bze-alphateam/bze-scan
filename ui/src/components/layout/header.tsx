"use client";

import { Box, CloseButton, Container, Drawer, Flex, HStack, IconButton, Link, Portal, Separator, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { LuMenu, LuSearch } from "react-icons/lu";

import { appLinks, navLinks } from "@/lib/links";

import { AppIcon } from "./app-icon";
import { AppsMenu } from "./apps-menu";
import { Logo } from "./logo";
import { SearchBox } from "./search-box";
import { ThemeToggle } from "./theme-toggle";

function isActive(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(`${href}/`);
}

/**
 * The header of every page. From `md` up: logo, search box, Apps, theme
 * toggle, and the nav row with the current section underlined. Below `md`: logo, a search icon that
 * opens the box under the bar, and a menu button opening a drawer with the
 * nav and the Apps links.
 */
export function Header() {
  const pathname = usePathname();
  const [searchOpen, setSearchOpen] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);

  return (
    <Box as="header" bg="bg.panel" borderBottomWidth="1px" borderColor="border.panel">
      <Container maxW="7xl" px={{ base: "4", md: "6" }}>
        <Flex minH="60px" align="center" gap="4">
          <Logo />
          <Box flex="1" maxW="520px" ms="2" display={{ base: "none", md: "block" }}>
            <SearchBox />
          </Box>
          <HStack gap="2" ms="auto" display={{ base: "none", md: "flex" }}>
            <AppsMenu />
            <ThemeToggle />
          </HStack>

          <HStack gap="1" ms="auto" display={{ base: "flex", md: "none" }}>
            <IconButton
              variant="ghost"
              minW="44px"
              minH="44px"
              aria-label={searchOpen ? "Close search" : "Open search"}
              aria-expanded={searchOpen}
              onClick={() => setSearchOpen((o) => !o)}
            >
              <LuSearch />
            </IconButton>
            <Drawer.Root open={drawerOpen} onOpenChange={(e) => setDrawerOpen(e.open)} placement="end">
              <Drawer.Trigger asChild>
                <IconButton variant="ghost" minW="44px" minH="44px" aria-label="Open menu">
                  <LuMenu />
                </IconButton>
              </Drawer.Trigger>
              <Portal>
                <Drawer.Backdrop />
                <Drawer.Positioner>
                  <Drawer.Content>
                    <Drawer.Header>
                      <Drawer.Title>Menu</Drawer.Title>
                    </Drawer.Header>
                    <Drawer.Body>
                      <Stack as="nav" aria-label="Explorer" gap="0">
                        {navLinks.map((link) => (
                          <Link
                            key={link.href}
                            asChild
                            minH="44px"
                            fontWeight={isActive(pathname, link.href) ? "semibold" : "normal"}
                            onClick={() => setDrawerOpen(false)}
                          >
                            <NextLink href={link.href}>{link.label}</NextLink>
                          </Link>
                        ))}
                      </Stack>
                      <Separator my="4" />
                      <Text fontSize="xs" fontWeight="semibold" color="fg.muted" mb="2">
                        APPS
                      </Text>
                      <Stack gap="0">
                        {appLinks.map((link) => (
                          <Link key={link.label} href={link.href} minH="44px" gap="2.5">
                            <AppIcon app={link.icon} size={20} />
                            {link.label}
                          </Link>
                        ))}
                      </Stack>
                      <Separator my="4" />
                      <ThemeToggle />
                    </Drawer.Body>
                    <Drawer.CloseTrigger asChild>
                      <CloseButton size="md" aria-label="Close menu" />
                    </Drawer.CloseTrigger>
                  </Drawer.Content>
                </Drawer.Positioner>
              </Portal>
            </Drawer.Root>
          </HStack>
        </Flex>

        {searchOpen && (
          <Box pb="3" display={{ base: "block", md: "none" }}>
            <SearchBox autoFocus onSubmitted={() => setSearchOpen(false)} />
          </Box>
        )}

        <HStack as="nav" aria-label="Explorer" gap="0.5" css={{ "& > a:first-of-type": { marginInlineStart: "-12px" } }} borderTopWidth="1px" borderColor="border.panel" display={{ base: "none", md: "flex" }}>
          {navLinks.map((link) => {
            const active = isActive(pathname, link.href);
            return (
              <Link
                key={link.href}
                asChild
                px="3"
                py="3"
                fontSize="13px"
                fontWeight="500"
                color={active ? "fg" : "fg.muted"}
                borderBottomWidth="2px"
                borderColor={active ? "accent.fg" : "transparent"}
                rounded="0"
                _hover={{ color: "fg", textDecoration: "none" }}
              >
                <NextLink href={link.href} aria-current={active ? "page" : undefined}>
                  {link.label}
                </NextLink>
              </Link>
            );
          })}
        </HStack>
      </Container>
    </Box>
  );
}
