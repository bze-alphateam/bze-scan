import { Box } from "@chakra-ui/react";

import { CopyButton } from "./copy-button";

/** Pretty-printed JSON with a Copy button. */
export function JsonBlock({ value, maxH = "480px" }: { value: unknown; maxH?: string }) {
  const text = JSON.stringify(value, null, 2) ?? "null";
  return (
    <Box position="relative" bg="bg.page" borderTopWidth="1px" borderColor="border.panel" color="fg.muted">
      <CopyButton value={text} label="Copy JSON" position="absolute" top="2" right="2" />
      <Box as="pre" fontFamily="mono" fontSize="12px" lineHeight="1.55" px="4" py="14px" pr="12" overflow="auto" maxH={maxH} whiteSpace="pre">
        {text}
      </Box>
    </Box>
  );
}
