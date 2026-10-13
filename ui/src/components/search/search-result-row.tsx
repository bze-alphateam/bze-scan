"use client";

import { Box, HStack, Stack, Text } from "@chakra-ui/react";
import { LuUser } from "react-icons/lu";

import type { SearchResult } from "@/api/types";
import { Avatar } from "@/components/ui/avatar";
import { Chip } from "@/components/ui/chip";
import { shortAddress } from "@/lib/format";

/**
 * One search result: an avatar, its name, its short address and a chip for
 * what it is ("validator", "account"); `open` is the "Open account" row of an
 * address typed in full, a warn "not seen yet" chip when the explorer has not
 * indexed it. `size` is the avatar's: "7" in the header box, "8" on the page.
 */
export function SearchResultRow({ r, open = false, size = "7" }: { r: SearchResult; open?: boolean; size?: string }) {
  const unseen = open && r.indexed === false;
  return (
    <HStack gap="3" minW="0" w="full">
      {open ? (
        <Box display="grid" placeItems="center" boxSize={size} rounded="l2" bg="bg.hover" color="fg.muted" flexShrink={0} aria-hidden>
          <LuUser size={size === "8" ? 20 : 16} />
        </Box>
      ) : (
        <Avatar name={r.label || r.id} size={size} />
      )}
      <Stack gap="0" minW="0" flex="1">
        <Text fontSize="sm" fontWeight="600" color="accent.fg" truncate>
          {open ? "Open account" : r.label || shortAddress(r.id)}
        </Text>
        <Text fontSize="xs" color="fg.muted" fontFamily="mono" truncate>
          {shortAddress(r.id)}
        </Text>
      </Stack>
      <Chip variant={unseen ? "warn" : "default"} flexShrink={0}>
        {unseen ? "not seen yet" : r.type}
      </Chip>
    </HStack>
  );
}
