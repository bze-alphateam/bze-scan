import { Box, type BoxProps } from "@chakra-ui/react";
import type { ReactNode } from "react";

export type TagTone = "accent" | "ok" | "bad" | "warn";

const TONES: Record<TagTone, BoxProps> = {
  accent: { bg: "accent.subtle", color: "accent.fg" },
  ok: { bg: "pos.subtle", color: "pos.fg" },
  bad: { bg: "neg.subtle", color: "neg.fg" },
  warn: { bg: "warn.subtle", color: "warn.fg" },
};

interface TagProps extends Omit<BoxProps, "children"> {
  tone?: TagTone;
  children: ReactNode;
}

/** The status of a page's subject (SUCCESS, ACTIVE, PASSED): small, upper case, on a tinted background. */
export function Tag({ tone = "accent", children, ...props }: TagProps) {
  return (
    <Box
      as="span"
      display="inline-flex"
      alignItems="center"
      gap="1"
      fontSize="11px"
      fontWeight="600"
      lineHeight="1.45"
      px="7px"
      py="2px"
      rounded="l1"
      textTransform="uppercase"
      letterSpacing="0.04em"
      whiteSpace="nowrap"
      data-tag={tone}
      {...TONES[tone]}
      {...props}
    >
      {children}
    </Box>
  );
}

interface StatusPillProps extends Omit<TagProps, "children" | "tone"> {
  success: boolean;
}

/** "Success" in green or "Failed" in red. */
export function StatusPill({ success, ...props }: StatusPillProps) {
  return (
    <Tag tone={success ? "ok" : "bad"} {...props}>
      {success ? "Success" : "Failed"}
    </Tag>
  );
}
