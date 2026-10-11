import { Box, Heading, SimpleGrid, Stack, Text } from "@chakra-ui/react";

import { SearchBox } from "@/components/layout/search-box";
import { TileSkeleton } from "@/components/ui/skeleton";

export default function HomePage() {
  return (
    <Stack gap={{ base: "8", md: "12" }}>
      <Stack as="section" gap="4" align="center" textAlign="center" py={{ base: "4", md: "8" }}>
        <Heading as="h1" size={{ base: "2xl", md: "4xl" }}>
          Explore the BeeZee network
        </Heading>
        <Text color="fg.muted" maxW="2xl">
          Look up any block, transaction, address or validator, and see in plain words what happened on chain.
        </Text>
        <Box w="full" maxW="2xl">
          <SearchBox size="lg" />
        </Box>
      </Stack>

      <Stack as="section" gap="3" aria-labelledby="being-built">
        <Text id="being-built" fontSize="sm" color="fg.muted">
          The explorer is being built: the network&apos;s numbers and the latest blocks and transactions land here
          soon.
        </Text>
        <SimpleGrid columns={{ base: 2, md: 3, lg: 6 }} gap="3">
          {Array.from({ length: 6 }).map((_, i) => (
            <TileSkeleton key={i} />
          ))}
        </SimpleGrid>
      </Stack>
    </Stack>
  );
}
