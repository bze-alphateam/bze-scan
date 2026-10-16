"use client";

import { Button, HStack, Menu, Portal } from "@chakra-ui/react";
import { LuChevronDown, LuExternalLink } from "react-icons/lu";

import { appLinks } from "@/lib/links";

import { AppIcon } from "./app-icon";

/** "Apps": the other BZE apps, opened in the same tab. */
export function AppsMenu() {
  return (
    <Menu.Root>
      <Menu.Trigger asChild>
        <Button variant="outline" size="md" px="3" gap="1.5" fontWeight="500">
          Apps
          <LuChevronDown />
        </Button>
      </Menu.Trigger>
      <Portal>
        <Menu.Positioner>
          <Menu.Content minW="44">
            {appLinks.map((link) => (
              <Menu.Item key={link.label} value={link.label} asChild minH="44px">
                <a href={link.href}>
                  <HStack as="span" gap="2.5">
                    <AppIcon app={link.icon} size={20} />
                    {link.label}
                  </HStack>
                  <Menu.ItemCommand>
                    <LuExternalLink />
                  </Menu.ItemCommand>
                </a>
              </Menu.Item>
            ))}
          </Menu.Content>
        </Menu.Positioner>
      </Portal>
    </Menu.Root>
  );
}
