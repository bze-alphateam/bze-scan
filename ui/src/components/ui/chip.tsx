import { Box, type BoxProps } from "@chakra-ui/react";
import type { ReactNode } from "react";
import { LuLock } from "react-icons/lu";

export type ChipVariant = "default" | "ok" | "acc" | "warn" | "bad" | "ch" | "via" | "ln" | "hist" | "modc";

const LOOKS: Record<ChipVariant, BoxProps> = {
  default: { bg: "bg.hover", color: "fg.muted" },
  ok: { bg: "pos.subtle", color: "pos.fg" },
  acc: { bg: "accent.subtle", color: "accent.fg" },
  warn: { bg: "warn.subtle", color: "warn.fg" },
  bad: { bg: "neg.subtle", color: "neg.fg" },
  // An origin chain, outlined.
  ch: { borderWidth: "1px", borderColor: "border.panel", color: "fg" },
  // "via X": the chain a token passed through, dashed.
  via: { borderWidth: "1px", borderStyle: "dashed", borderColor: "fg.muted", color: "fg.muted" },
  // An outlined accent link count: "2 hops".
  ln: { borderWidth: "1px", borderColor: "accent.fg", color: "accent.fg" },
  // "historical": data from a snapshot, explained on hover.
  hist: { borderWidth: "1px", borderColor: "border.panel", color: "fg.muted", cursor: "help" },
  // "Module account": dashed with a lock.
  modc: { borderWidth: "1px", borderStyle: "dashed", borderColor: "fg.muted", color: "fg.muted" },
};

interface ChipProps extends Omit<BoxProps, "children"> {
  variant?: ChipVariant;
  children: ReactNode;
}

/** A descriptive label (Native, Factory, an origin chain): normal case, unlike a status `Tag`. */
export function Chip({ variant = "default", children, ...props }: ChipProps) {
  return (
    <Box
      as="span"
      display="inline-flex"
      alignItems="center"
      gap="5px"
      fontSize="11px"
      fontWeight="500"
      lineHeight="1.45"
      px="6px"
      py="1px"
      rounded="l1"
      whiteSpace="nowrap"
      data-chip={variant}
      {...LOOKS[variant]}
      {...props}
    >
      {variant === "modc" && <LuLock size={10} aria-hidden />}
      {children}
    </Box>
  );
}
