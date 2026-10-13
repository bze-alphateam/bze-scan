import { Flex, Text, type FlexProps } from "@chakra-ui/react";
import type { ReactNode } from "react";

export type CalloutTone = "acc" | "ok" | "warn" | "bad";

const EDGE: Record<CalloutTone, { edge: string; bg: string }> = {
  acc: { edge: "accent.fg", bg: "accent.subtle" },
  ok: { edge: "pos.fg", bg: "pos.subtle" },
  warn: { edge: "warn.fg", bg: "warn.subtle" },
  bad: { edge: "neg.fg", bg: "neg.subtle" },
};

interface CalloutProps extends Omit<FlexProps, "title"> {
  tone?: CalloutTone;
  /** The bold first line. */
  title: ReactNode;
  children?: ReactNode;
}

/** A note on a detail page that says one thing out loud: a 3px edge in its tone, a bold title, then a line. */
export function Callout({ tone = "acc", title, children, ...props }: CalloutProps) {
  const look = EDGE[tone];
  return (
    <Flex
      direction="column"
      gap="1"
      px="4"
      py="3"
      borderWidth="1px"
      borderColor="border.panel"
      borderLeftWidth="3px"
      borderLeftColor={look.edge}
      rounded="l2"
      bg={look.bg}
      fontSize="13px"
      lineHeight="1.55"
      data-tone={tone}
      {...props}
    >
      <Text as="b" fontWeight="600">
        {title}
      </Text>
      {children && <Text as="div">{children}</Text>}
    </Flex>
  );
}
