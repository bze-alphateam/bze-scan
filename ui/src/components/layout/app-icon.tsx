import { Box, Image } from "@chakra-ui/react";
import { LuBookOpen } from "react-icons/lu";

import { appIdentity, type AppLinkIcon } from "@/lib/links";

interface AppIconProps {
  app: AppLinkIcon;
  /** Width and height in px. 32 and below use the small cut. */
  size: number;
}

/**
 * The mark in front of a link to a BZE app. Decorative: the label next to it
 * names the link. The light and dark files switch in CSS, like the logo, so
 * there is no flash.
 */
export function AppIcon({ app, size }: AppIconProps) {
  if (app === "website") {
    return <Image src="/images/bze_alternative_512x512.png" alt="" aria-hidden boxSize={`${size}px`} flexShrink={0} data-testid="app-icon" data-app={app} />;
  }
  if (app === "docs") {
    return (
      <Box
        as="span"
        display="inline-grid"
        placeItems="center"
        boxSize={`${size}px`}
        flexShrink={0}
        rounded="full"
        bg="bg.muted"
        color="fg.muted"
        aria-hidden
        data-testid="app-icon"
        data-app={app}
      >
        <LuBookOpen size={Math.round(size * 0.55)} />
      </Box>
    );
  }
  const files = size <= 32 ? appIdentity[app].iconSmall : appIdentity[app].icon;
  return (
    <Box as="span" display="inline-flex" boxSize={`${size}px`} flexShrink={0} aria-hidden data-testid="app-icon" data-app={app}>
      <Image src={files.light} alt="" boxSize={`${size}px`} display={{ base: "block", _dark: "none" }} />
      <Image src={files.dark} alt="" boxSize={`${size}px`} display={{ base: "none", _dark: "block" }} />
    </Box>
  );
}
