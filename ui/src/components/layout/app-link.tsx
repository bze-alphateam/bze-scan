import { IconButton, Link } from "@chakra-ui/react";
import type { ReactNode } from "react";
import { LuExternalLink } from "react-icons/lu";

import { appIdentity, type AppKey } from "@/lib/links";

import { AppIcon } from "./app-icon";

function accentOf(app: AppKey) {
  return { base: appIdentity[app].accent.light, _dark: appIdentity[app].accent.dark };
}

interface AppLinkProps {
  /** The BZE app the link opens. */
  app: AppKey;
  href: string;
  /** The app's name only ("DEX", "Staking"); a verb stays as text before the link. */
  children: ReactNode;
}

/**
 * The one way to point at another BZE app, as in the dapps' ui-kit: a ghost
 * link (no border, no fill) with the app's icon, its name in the app's accent
 * and an external-link mark, opening in a new tab.
 */
export function AppLink({ app, href, children }: AppLinkProps) {
  return (
    <Link
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      color={accentOf(app)}
      fontSize="sm"
      fontWeight="medium"
      lineHeight="1.25rem"
      display="inline-flex"
      alignItems="center"
      gap="1.5"
      px="1.5"
      py="1"
      borderRadius="md"
      verticalAlign="middle"
      whiteSpace="nowrap"
      textDecoration="none"
      _hover={{ bg: "bg.subtle", textDecoration: "none", color: accentOf(app) }}
      data-testid="app-link"
      data-app={app}
    >
      <AppIcon app={app} size={16} />
      {children}
      <LuExternalLink aria-hidden size={12} />
    </Link>
  );
}

interface AppIconButtonProps {
  app: AppKey;
  href: string;
  /** What the link opens, e.g. "Trade on DEX": the only name it has. */
  "aria-label": string;
  size?: "xs" | "sm" | "md";
}

/** The icon-only AppLink, for rows with no room for a label. */
export function AppIconButton({ app, href, "aria-label": label, size = "sm" }: AppIconButtonProps) {
  return (
    <IconButton asChild variant="ghost" size={size} _hover={{ bg: "bg.subtle" }} aria-label={label} title={label} data-testid="app-icon-button" data-app={app}>
      <a href={href} target="_blank" rel="noopener noreferrer">
        <AppIcon app={app} size={size === "md" ? 18 : 16} />
      </a>
    </IconButton>
  );
}
