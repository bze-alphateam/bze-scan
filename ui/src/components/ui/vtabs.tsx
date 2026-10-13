"use client";

import { Box, Flex, Text, type FlexProps } from "@chakra-ui/react";
import { useRef, type KeyboardEvent, type ReactNode } from "react";

export interface VTab<T extends string> {
  value: T;
  label: ReactNode;
  /** The muted mono number after the label; hidden while null. */
  count?: ReactNode;
  /** Before the label (the Hall of Fame crown). */
  icon?: ReactNode;
}

interface VTabsProps<T extends string> {
  /** The ids' base: tab `{id}-tab-{value}`, panel `{id}-panel`. */
  id: string;
  label: string;
  tabs: VTab<T>[];
  value: T;
  onChange: (value: T) => void;
}

/**
 * The tabs over a list (Hall of Fame / Active / Inactive): 13px labels with
 * a muted count, the chosen one underlined in the accent. Arrow keys, Home
 * and End move between them.
 */
export function VTabs<T extends string>({ id, label, tabs, value, onChange }: VTabsProps<T>) {
  const list = useRef<HTMLDivElement>(null);
  const move = (e: KeyboardEvent) => {
    const at = tabs.findIndex((t) => t.value === value);
    const to =
      e.key === "ArrowRight"
        ? (at + 1) % tabs.length
        : e.key === "ArrowLeft"
          ? (at - 1 + tabs.length) % tabs.length
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? tabs.length - 1
              : null;
    if (to == null) return;
    e.preventDefault();
    onChange(tabs[to].value);
    list.current?.querySelectorAll<HTMLElement>('[role="tab"]')[to]?.focus();
  };
  return (
    <Flex ref={list} role="tablist" aria-label={label} gap="2px" wrap="wrap" onKeyDown={move}>
      {tabs.map((t, i) => {
        const on = t.value === value;
        return (
          <Box
            key={t.value}
            as="button"
            {...{ type: "button" }}
            role="tab"
            id={`${id}-tab-${t.value}`}
            aria-selected={on}
            aria-controls={`${id}-panel`}
            tabIndex={on ? 0 : -1}
            display="inline-flex"
            alignItems="center"
            gap="6px"
            bg="transparent"
            border="0"
            borderBottomWidth="2px"
            borderBottomColor={on ? "accent.fg" : "transparent"}
            color={on ? "fg" : "fg.muted"}
            fontSize="13px"
            fontWeight="500"
            px="3"
            py="10px"
            mb="-1px"
            ms={i === 0 ? "-3" : undefined}
            cursor="pointer"
            whiteSpace="nowrap"
            _hover={{ color: "fg" }}
            onClick={() => onChange(t.value)}
          >
            {t.icon}
            {t.label}
            {t.count != null && (
              <>
                {" "}
                <Text as="span" fontFamily="mono" fontSize="12px" color="fg.muted">
                  {t.count}
                </Text>
              </>
            )}
          </Box>
        );
      })}
    </Flex>
  );
}

/** The row holding `VTabs` on the left and a filter on the right, over a bottom line. */
export function VTabsBar(props: FlexProps) {
  return (
    <Flex
      align="flex-end"
      justify="space-between"
      columnGap="4"
      rowGap="3"
      wrap="wrap"
      borderBottomWidth="1px"
      borderColor="border.panel"
      mb="14px"
      {...props}
    />
  );
}

/** The panel of the chosen tab. */
export function VTabPanel({ id, value, children }: { id: string; value: string; children: ReactNode }) {
  return (
    <Box role="tabpanel" id={`${id}-panel`} aria-labelledby={`${id}-tab-${value}`}>
      {children}
    </Box>
  );
}
