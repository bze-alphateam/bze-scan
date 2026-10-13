"use client";

import { Button } from "@chakra-ui/react";
import NextLink from "next/link";

import { LostPage } from "@/components/layout/lost-page";

export default function ErrorPage({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <LostPage
      code="error"
      title="Something went wrong"
      actions={
        <>
          <Button colorPalette="blue" h="40px" onClick={reset}>
            Try again
          </Button>
          <Button asChild variant="outline" h="40px">
            <NextLink href="/">Back to the explorer</NextLink>
          </Button>
        </>
      }
    >
      This page failed to load. Try again in a moment.
    </LostPage>
  );
}
