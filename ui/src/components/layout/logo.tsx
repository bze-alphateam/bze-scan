import { Box, HStack, Image, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";

/** The BeeZee logo for the colour mode (switched in CSS, so no flash) and the EXPLORER wordmark. */
export function Logo() {
  return (
    <Link asChild _hover={{ textDecoration: "none" }} minH="44px" aria-label="BZE Explorer home">
      <NextLink href="/">
        <HStack gap="2.5">
          <Box h="22px">
            <Image src="/images/beezee_dark.svg" alt="" h="22px" w="auto" display={{ base: "block", _dark: "none" }} />
            <Image src="/images/beezee_light.svg" alt="" h="22px" w="auto" display={{ base: "none", _dark: "block" }} />
          </Box>
          <Text as="span" fontSize="11px" fontWeight="600" letterSpacing="0.14em" color="fg.muted">
            EXPLORER
          </Text>
        </HStack>
      </NextLink>
    </Link>
  );
}
