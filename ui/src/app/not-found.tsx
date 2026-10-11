import { Button, Heading, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";

export default function NotFound() {
  return (
    <Stack gap="4" align="center" textAlign="center" py="16">
      <Heading as="h1" size="2xl">
        Nothing here
      </Heading>
      <Text color="fg.muted">This page does not exist, or not yet.</Text>
      <Button asChild colorPalette="blue" minH="44px">
        <NextLink href="/">Back to the explorer</NextLink>
      </Button>
    </Stack>
  );
}
