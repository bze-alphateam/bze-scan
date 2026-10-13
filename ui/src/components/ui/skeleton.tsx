"use client";

import { Box, HStack, Skeleton, SkeletonText, Stack } from "@chakra-ui/react";

// Skeleton twins of the page atoms (UI Design, "Loading states"): each takes
// the size of what it stands in for, so nothing shifts when the data lands.
// Page tickets show them on a first load only; refetches keep the old data.

const panel = {
  bg: "bg.panel",
  borderWidth: "1px",
  borderColor: "border.panel",
  rounded: "l3",
} as const;

/** One home tile: a label, a large value and a sub-line. */
export function TileSkeleton() {
  return (
    <Box {...panel} p="4" minH="108px" aria-hidden data-testid="tile-skeleton">
      <Skeleton h="3" w="40%" />
      <Skeleton h="7" w="70%" mt="3" />
      <Skeleton h="3" w="50%" mt="3" />
    </Box>
  );
}

/** A `Card`: a title line, a meta line and a few lines of body. */
export function CardSkeleton({ lines = 4 }: { lines?: number }) {
  return (
    <Box {...panel} p="5" aria-hidden data-testid="card-skeleton">
      <Skeleton h="5" w="35%" />
      <Skeleton h="3" w="55%" mt="2" />
      <SkeletonText noOfLines={lines} gap="3" mt="5" />
    </Box>
  );
}

/** A table: a header row and `rows` rows of `columns` cells, each 44 px tall like a real row. */
export function TableSkeleton({ rows, columns }: { rows: number; columns: number }) {
  const cells = Array.from({ length: columns });
  return (
    <Stack gap="0" {...panel} overflow="hidden" aria-hidden data-testid="table-skeleton">
      <HStack gap="4" px="4" h="10" borderBottomWidth="1px" borderColor="border.panel">
        {cells.map((_, c) => (
          <Skeleton key={c} h="3" flex="1" />
        ))}
      </HStack>
      {Array.from({ length: rows }).map((_, r) => (
        <HStack
          key={r}
          gap="4"
          px="4"
          h="44px"
          borderBottomWidth={r === rows - 1 ? "0" : "1px"}
          borderColor="border.panel"
          data-testid="table-skeleton-row"
        >
          {cells.map((_, c) => (
            <Skeleton key={c} h="4" flex="1" />
          ))}
        </HStack>
      ))}
    </Stack>
  );
}

/** A list of `rows` items: an avatar, a title and a sub-line each, as the home lists and stacked phone cards. */
export function ListSkeleton({ rows }: { rows: number }) {
  return (
    <Stack gap="0" {...panel} aria-hidden data-testid="list-skeleton">
      {Array.from({ length: rows }).map((_, r) => (
        <HStack
          key={r}
          gap="3"
          px="4"
          minH="64px"
          borderBottomWidth={r === rows - 1 ? "0" : "1px"}
          borderColor="border.panel"
          data-testid="list-skeleton-row"
        >
          <Skeleton boxSize="8" rounded="l2" />
          <Stack flex="1" gap="2">
            <Skeleton h="4" w="60%" />
            <Skeleton h="3" w="40%" />
          </Stack>
          <Skeleton h="4" w="20%" />
        </HStack>
      ))}
    </Stack>
  );
}

/** A `JsonBlock` while its raw JSON loads. */
export function JsonSkeleton({ lines = 10 }: { lines?: number }) {
  return (
    <Box {...panel} p="4" aria-hidden data-testid="json-skeleton">
      <SkeletonText noOfLines={lines} gap="2" />
    </Box>
  );
}
