"use client";

import { HStack, Stack, Text } from "@chakra-ui/react";
import { LuUser } from "react-icons/lu";

import type { SearchResult } from "@/api/types";
import { Avatar } from "@/components/ui/avatar";
import { shortAddress } from "@/lib/format";

/**
 * One search result: an avatar, its name, its short address and what it is
 * ("validator", "account"); `open` is the "Open account" row of an address
 * typed in full, "not seen yet" when the explorer has not indexed it.
 */
export function SearchResultRow({ r, open = false }: { r: SearchResult; open?: boolean }) {
  return (
    <HStack gap="3" minW="0" w="full">
      {open ? (
        <HStack justify="center" boxSize="7" rounded="full" bg="bg.muted" flexShrink={0} aria-hidden>
          <LuUser />
        </HStack>
      ) : (
        <Avatar name={r.label || r.id} size="7" />
      )}
      <Stack gap="0" minW="0" flex="1">
        <Text fontSize="sm" fontWeight="medium" truncate>
          {open ? "Open account" : r.label || shortAddress(r.id)}
        </Text>
        <Text fontSize="xs" color="fg.muted" fontFamily="mono" truncate>
          {shortAddress(r.id)}
        </Text>
      </Stack>
      <Text fontSize="xs" color="fg.muted" flexShrink={0}>
        {open && r.indexed === false ? "not seen yet" : r.type}
      </Text>
    </HStack>
  );
}
