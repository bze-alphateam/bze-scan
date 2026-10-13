import { Box, Text } from "@chakra-ui/react";
import type { ReactNode } from "react";

interface TileProps {
  label: string;
  /** Null while unknown: shown as `—`. */
  value: ReactNode | null;
  /** The grey sub-line. */
  sub?: ReactNode;
}

/** A summary tile: a small upper-case label, a large mono value and a sub-line; `TileSkeleton` is its twin. */
export function Tile({ label, value, sub }: TileProps) {
  return (
    <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" px="4" py="14px" minH="108px" data-testid="tile">
      <Text textStyle="tileLabel">{label}</Text>
      <Text textStyle="tileValue" mt="6px">
        {value ?? "—"}
      </Text>
      {sub && (
        <Text fontSize="12px" color="fg.muted" mt="1">
          {sub}
        </Text>
      )}
    </Box>
  );
}
