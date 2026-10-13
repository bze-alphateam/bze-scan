import { Box, Heading, HStack, Text } from "@chakra-ui/react";
import type { ReactNode } from "react";

import { SearchBox } from "./search-box";

// Custom breakpoints are not in Chakra's generated types, so responsive objects using them are hoisted.
const TOP = { base: "64px", c2: "96px" };

interface LostPageProps {
  /** The small outlined code tag: "404", "error". */
  code: string;
  title: string;
  children: ReactNode;
  /** The buttons under the line. */
  actions: ReactNode;
}

/** The app-wide not-found and error pages: a code tag, a heading, one line, the actions and a search box. */
export function LostPage({ code, title, children, actions }: LostPageProps) {
  return (
    <Box display="flex" flexDirection="column" alignItems="center" textAlign="center" pt={TOP} pb="72px">
      <Text
        as="span"
        fontFamily="mono"
        fontSize="12px"
        letterSpacing="0.14em"
        textTransform="uppercase"
        color="accent.fg"
        px="10px"
        py="3px"
        borderWidth="1px"
        borderColor="accent.fg"
        rounded="l1"
        data-testid="lost-code"
      >
        {code}
      </Text>
      <Heading as="h1" mt="18px" mb="2" fontSize="32px" fontWeight="600" letterSpacing="-0.01em" lineHeight="1.2">
        {title}
      </Heading>
      <Text fontSize="15px" color="fg.muted">
        {children}
      </Text>
      <HStack gap="10px" mt="6" wrap="wrap" justify="center">
        {actions}
      </HStack>
      <Box w="full" maxW="520px" mt="10" textAlign="start">
        <SearchBox placeholder="Or search by block, transaction hash, address or validator" />
      </Box>
    </Box>
  );
}
