import { Button } from "@chakra-ui/react";
import NextLink from "next/link";

import { LostPage } from "@/components/layout/lost-page";

export default function NotFound() {
  return (
    <LostPage
      code="404"
      title="Nothing here"
      actions={
        <Button asChild colorPalette="blue" h="40px">
          <NextLink href="/">Back to the explorer</NextLink>
        </Button>
      }
    >
      This page does not exist, or not yet.
    </LostPage>
  );
}
