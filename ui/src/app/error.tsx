"use client";

import { Button, Heading, Stack, Text } from "@chakra-ui/react";

export default function ErrorPage({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <Stack gap="4" align="center" textAlign="center" py="16">
      <Heading as="h1" size="2xl">
        Something went wrong
      </Heading>
      <Text color="fg.muted">This page failed to load. Try again in a moment.</Text>
      <Button colorPalette="blue" minH="44px" onClick={reset}>
        Try again
      </Button>
    </Stack>
  );
}
