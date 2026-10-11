import { Box, HStack, Image, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";

/** The BeeZee logo for the colour mode (switched in CSS, so no flash) and the EXPLORER wordmark. */
export function Logo() {
  return (
    <Link asChild _hover={{ textDecoration: "none" }} minH="44px" aria-label="BZE Explorer home">
      <NextLink href="/">
        <HStack gap="2">
          <Box h="28px">
            <Image src="/images/beezee_dark.svg" alt="" h="28px" w="auto" display={{ base: "block", _dark: "none" }} />
            <Image src="/images/beezee_light.svg" alt="" h="28px" w="auto" display={{ base: "none", _dark: "block" }} />
          </Box>
          <Text as="span" fontSize="xs" fontWeight="bold" letterSpacing="widest" color="fg.muted">
            EXPLORER
          </Text>
        </HStack>
      </NextLink>
    </Link>
  );
}
