"use client";

import { Box, Text, type BoxProps } from "@chakra-ui/react";

import type { StakeChangeLine } from "@/render/validators";

/** `24h ▲ +122.0K`: the window muted, then the arrow and the amount in the growth or shrink tone. */
function Line({ line }: { line: StakeChangeLine }) {
  return (
    <Text as="span" whiteSpace="nowrap" data-testid="stake-change-line" data-window={line.window} data-dir={line.up ? "up" : "down"}>
      <Text as="span" color="fg.muted">
        {line.window}
      </Text>{" "}
      <Text as="span" color={line.up ? "pos.fg" : "warn.fg"}>
        {line.up ? "▲" : "▼"} {line.text}
      </Text>
    </Text>
  );
}

/**
 * A validator's stake change: one line per window (`stacked`, the list's
 * Change column) or both on one line. Nothing at all without a line to show.
 */
export function StakeChangeText({ lines, stacked, ...props }: { lines: StakeChangeLine[]; stacked?: boolean } & BoxProps) {
  if (lines.length === 0) return null;
  return (
    <Box
      as="span"
      display={stacked ? "flex" : "inline-flex"}
      flexDirection={stacked ? "column" : "row"}
      alignItems={stacked ? "flex-end" : "baseline"}
      columnGap="3"
      rowGap="2px"
      flexWrap="wrap"
      fontFamily="mono"
      fontSize="12px"
      data-testid="stake-change"
      {...props}
    >
      {lines.map((l) => (
        <Line key={l.window} line={l} />
      ))}
    </Box>
  );
}
