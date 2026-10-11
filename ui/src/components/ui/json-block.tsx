import { Box } from "@chakra-ui/react";

import { CopyButton } from "./copy-button";

/** Pretty-printed JSON with a Copy button. */
export function JsonBlock({ value, maxH = "480px" }: { value: unknown; maxH?: string }) {
  const text = JSON.stringify(value, null, 2) ?? "null";
  return (
    <Box position="relative" bg="bg.subtle" borderWidth="1px" borderColor="border.panel" rounded="l2">
      <CopyButton value={text} label="Copy JSON" position="absolute" top="2" right="2" />
      <Box as="pre" fontFamily="mono" fontSize="xs" p="4" pr="12" overflow="auto" maxH={maxH} whiteSpace="pre">
        {text}
      </Box>
    </Box>
  );
}
