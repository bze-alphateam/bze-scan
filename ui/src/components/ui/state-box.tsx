import { Box, Flex, Text, type FlexProps } from "@chakra-ui/react";
import type { ReactNode } from "react";

export type StateTone = "info" | "warn" | "error" | "neutral";

const TONES: Record<StateTone, { bg: string; color: string }> = {
  info: { bg: "accent.subtle", color: "accent.fg" },
  warn: { bg: "warn.subtle", color: "warn.fg" },
  error: { bg: "neg.subtle", color: "neg.fg" },
  neutral: { bg: "bg.hover", color: "fg.muted" },
};

interface StateBoxProps extends Omit<FlexProps, "title"> {
  /** info: waiting · warn: a gap in what the explorer indexed · error · neutral: not found. */
  tone?: StateTone;
  icon: ReactNode;
  title: ReactNode;
  /** One muted line saying what happened or what to do. */
  children?: ReactNode;
  /** Links and buttons under the line. */
  actions?: ReactNode;
}

/** Not found, empty and error states: an icon tile, a bold title, one muted line and the actions. */
export function StateBox({ tone = "neutral", icon, title, children, actions, ...props }: StateBoxProps) {
  const look = TONES[tone];
  return (
    <Flex
      gap="14px"
      align="flex-start"
      px="5"
      py="18px"
      maxW="880px"
      bg="bg.panel"
      borderWidth="1px"
      borderColor="border.panel"
      borderLeftWidth={tone === "info" ? "2px" : "1px"}
      borderLeftColor={tone === "info" ? "accent.fg" : "border.panel"}
      rounded="l3"
      data-state={tone}
      {...props}
    >
      <Box flex="none" display="grid" placeItems="center" boxSize="40px" rounded="l3" bg={look.bg} color={look.color} aria-hidden>
        {icon}
      </Box>
      <Box minW="0" pt="2px">
        <Text as="h2" fontWeight="600" fontSize="15px">
          {title}
        </Text>
        {children && (
          <Text mt="1" color="fg.muted" fontSize="13px" lineHeight="1.55">
            {children}
          </Text>
        )}
        {actions && (
          <Flex align="center" gap="14px" wrap="wrap" mt="3" fontSize="13px">
            {actions}
          </Flex>
        )}
      </Box>
    </Flex>
  );
}
