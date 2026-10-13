import { Box, Text, type BoxProps } from "@chakra-ui/react";
import type { ReactNode } from "react";

interface TileProps extends Omit<BoxProps, "children"> {
  label: string;
  /** Null while unknown: shown as `—`. */
  value: ReactNode | null;
  /** The grey sub-line. */
  sub?: ReactNode;
  /** The value's colour: `pos.fg` for a good outcome, `neg.fg` for a bad one. */
  valueColor?: string;
  /** A 16px value for words and dates instead of the large number. */
  small?: boolean;
  /** The accent border and value: what the reader is waiting for. */
  highlight?: boolean;
}

/** A summary tile: a small upper-case label, a large mono value and a sub-line; `TileSkeleton` is its twin. */
export function Tile({ label, value, sub, valueColor, small, highlight, ...props }: TileProps) {
  return (
    <Box
      bg="bg.panel"
      borderWidth="1px"
      borderColor={highlight ? "accent.fg" : "border.panel"}
      rounded="l3"
      px="4"
      py="14px"
      minH="108px"
      minW="0"
      data-testid="tile"
      {...props}
    >
      <Text textStyle="tileLabel">{label}</Text>
      <Text
        as="div"
        textStyle="tileValue"
        mt={small ? "9px" : "6px"}
        fontSize={small ? "16px" : undefined}
        color={highlight ? "accent.fg" : valueColor}
        overflowWrap="anywhere"
      >
        {value ?? "—"}
      </Text>
      {sub && (
        <Text as="div" fontSize="12px" color="fg.muted" mt="1" lineHeight="1.45">
          {sub}
        </Text>
      )}
    </Box>
  );
}
