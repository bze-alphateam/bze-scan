import { Box, Text } from "@chakra-ui/react";
import type { ReactNode } from "react";

interface TileProps {
  label: string;
  /** Null while unknown: shown as `—`. */
  value: ReactNode | null;
  /** The grey sub-line. */
  sub?: ReactNode;
}

/** A summary tile: a label, a large value and a sub-line; `TileSkeleton` is its twin. */
export function Tile({ label, value, sub }: TileProps) {
  return (
    <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" p="4" minH="108px" data-testid="tile">
      <Text fontSize="sm" color="fg.muted">
        {label}
      </Text>
      <Text fontSize="2xl" fontWeight="semibold" mt="1" lineHeight="short">
        {value ?? "—"}
      </Text>
      {sub && (
        <Text fontSize="sm" color="fg.muted" mt="1">
          {sub}
        </Text>
      )}
    </Box>
  );
}
