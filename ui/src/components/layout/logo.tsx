import { Box, Image, Link } from "@chakra-ui/react";
import NextLink from "next/link";

const LOGO = "/images/explorer";

/**
 * The BZE Explorer lockup: landscape (icon, name, "by beezee") from `md` up,
 * compact (icon and name) on phones. The light and dark files switch in CSS,
 * so there is no flash.
 */
export function Logo() {
  return (
    <Link asChild _hover={{ textDecoration: "none" }} minH="44px" aria-label="BZE Explorer home">
      <NextLink href="/">
        <Box display={{ base: "none", md: "block" }} h="40px" data-testid="logo-landscape">
          <Image src={`${LOGO}/landscape-light.svg`} alt="" h="40px" w="auto" display={{ base: "block", _dark: "none" }} />
          <Image src={`${LOGO}/landscape-dark.svg`} alt="" h="40px" w="auto" display={{ base: "none", _dark: "block" }} />
        </Box>
        <Box display={{ base: "block", md: "none" }} h="32px" data-testid="logo-compact">
          <Image src={`${LOGO}/compact-light.svg`} alt="" h="32px" w="auto" display={{ base: "block", _dark: "none" }} />
          <Image src={`${LOGO}/compact-dark.svg`} alt="" h="32px" w="auto" display={{ base: "none", _dark: "block" }} />
        </Box>
      </NextLink>
    </Link>
  );
}
