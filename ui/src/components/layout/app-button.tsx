import { Button } from "@chakra-ui/react";
import type { ReactNode } from "react";
import { LuExternalLink } from "react-icons/lu";

import { appIdentity, type AppKey } from "@/lib/links";

import { AppIcon } from "./app-icon";

interface AppButtonProps {
  app: AppKey;
  href: string;
  children: ReactNode;
}

/**
 * A page's action that opens another BZE app. Outlined in the app's colour,
 * not filled: the app icon is drawn for the page background and fades on a
 * coloured fill.
 */
export function AppButton({ app, href, children }: AppButtonProps) {
  const accent = { base: appIdentity[app].accent.light, _dark: appIdentity[app].accent.dark };
  return (
    <Button
      asChild
      variant="outline"
      h="40px"
      px="4"
      fontWeight="600"
      color={accent}
      borderColor={accent}
      _hover={{ bg: "bg.muted" }}
      data-testid="app-button"
      data-app={app}
    >
      <a href={href} target="_blank" rel="noopener noreferrer">
        <AppIcon app={app} size={18} />
        {children} <LuExternalLink aria-hidden />
      </a>
    </Button>
  );
}
