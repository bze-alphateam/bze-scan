"use client";

import { ClientOnly, IconButton, Skeleton } from "@chakra-ui/react";
import { LuMoon, LuSun } from "react-icons/lu";

import { useColorMode } from "@/components/ui/color-mode";

/** Switches between dark (the default) and light; next-themes remembers the choice. */
export function ThemeToggle() {
  return (
    <ClientOnly fallback={<Skeleton boxSize="10" rounded="l2" />}>
      <Toggle />
    </ClientOnly>
  );
}

function Toggle() {
  const { colorMode, toggleColorMode } = useColorMode();
  const dark = colorMode === "dark";
  return (
    <IconButton
      variant="outline"
      size="md"
      aria-label={dark ? "Switch to light mode" : "Switch to dark mode"}
      onClick={toggleColorMode}
    >
      {dark ? <LuSun /> : <LuMoon />}
    </IconButton>
  );
}
