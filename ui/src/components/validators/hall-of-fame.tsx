"use client";

import { Box, Link, Text, type BoxProps } from "@chakra-ui/react";
import NextLink from "next/link";
import { useId, type ReactNode } from "react";
import { LuCrown } from "react-icons/lu";

import type { HallOfFameRules, Validator } from "@/api/types";
import { useHoverCard } from "@/components/ui/hover-card";
import { fameMissedText, recentVotesText } from "@/render/validators";

/** The crown: the Hall of Fame's mark, in the warn tone. Never an emoji. */
export function Crown({ size = 14 }: { size?: number }) {
  return (
    <Box as="span" display="inline-flex" color="warn.fg" flex="none" aria-hidden>
      <LuCrown size={size} />
    </Box>
  );
}

/** A Hall of Fame member's moniker: the gold gradient text. Gold is reserved for members. */
export function GoldName({ children, ...props }: BoxProps) {
  return (
    <Box as="span" bgImage="{colors.gold.name}" bgClip="text" color="transparent" data-gold {...props}>
      {children}
    </Box>
  );
}

/** A validator's moniker, gold for a Hall of Fame member. */
export function Moniker({ name, gold }: { name: string; gold: boolean }) {
  return gold ? <GoldName>{name}</GoldName> : <>{name}</>;
}

interface CrownMarkProps {
  v: Pick<Validator, "missed_blocks" | "signed_blocks_window" | "recent_votes">;
  rules: HallOfFameRules;
  /** "See the Hall of Fame": a tab switch on the list, a link elsewhere. */
  seeAll: { onClick: () => void } | { href: string };
  size?: number;
}

/**
 * The crown after a member's moniker: a button that opens a small card on
 * hover, on keyboard focus and on tap, saying why the validator is in the
 * Hall of Fame with its own figures and the rule's thresholds.
 */
export function CrownMark({ v, rules, seeAll, size = 13 }: CrownMarkProps) {
  const id = useId();
  const { open, close, toggle, rootProps } = useHoverCard();

  return (
    <Box
      as="span"
      {...rootProps}
      position="relative"
      display="inline-flex"
      verticalAlign="-2px"
      ms="6px"
      data-testid="fame-crown"
    >
      <Box
        as="button"
        {...{ type: "button" }}
        display="inline-flex"
        p="0"
        bg="transparent"
        border="0"
        cursor="help"
        rounded="3px"
        _focusVisible={{
          outline: "none",
          boxShadow: "0 0 0 2px {colors.accent.fg}",
        }}
        aria-label="Hall of Fame member"
        aria-describedby={id}
        aria-expanded={open}
        onClick={toggle}
      >
        <Crown size={size} />
      </Box>
      <Box
        as="span"
        id={id}
        role="tooltip"
        hidden={!open}
        position="absolute"
        left="50%"
        top="calc(100% + 8px)"
        transform="translateX(-50%)"
        zIndex="popover"
        w="280px"
        maxW="calc(100vw - 32px)"
        px="14px"
        py="3"
        bg="bg.panel"
        borderWidth="1px"
        borderColor="gold.border"
        rounded="l3"
        boxShadow="0 8px 24px rgba(0,0,0,.35)"
        fontSize="12px"
        lineHeight="1.5"
        fontWeight="400"
        color="fg"
        textAlign="start"
        whiteSpace="normal"
        cursor="default"
        // The gap above the card keeps the hover while the pointer crosses it.
        _before={{
          content: '""',
          position: "absolute",
          insetX: "0",
          top: "-10px",
          h: "10px",
        }}
        css={{
          "&:not([hidden])": {
            display: "flex",
            flexDirection: "column",
            gap: "5px",
          },
        }}
        data-testid="fame-card"
      >
        <Box as="span" display="flex" alignItems="center" gap="6px" fontWeight="600" fontSize="13px" color="warn.fg" mb="2px">
          <Crown size={13} />
          Hall of Fame
        </Box>
        <Check>{recentVotesText(v)}</Check>
        <Check>{fameMissedText(v, rules)}</Check>
        <Text as="span" color="fg.muted" mt="2px">
          Active validators that vote on the latest proposals and almost never miss a block.
        </Text>
        {"href" in seeAll ? (
          <Link asChild color="accent.fg" alignSelf="flex-start" mt="2px">
            <NextLink href={seeAll.href}>See the Hall of Fame →</NextLink>
          </Link>
        ) : (
          <Box
            as="button"
            {...{ type: "button" }}
            alignSelf="flex-start"
            mt="2px"
            p="0"
            bg="transparent"
            border="0"
            color="accent.fg"
            font="inherit"
            cursor="pointer"
            _hover={{ textDecoration: "underline" }}
            onClick={() => {
              close();
              seeAll.onClick();
            }}
          >
            See the Hall of Fame →
          </Box>
        )}
      </Box>
    </Box>
  );
}

function Check({ children }: { children: ReactNode }) {
  return (
    <Box as="span" display="flex" gap="2">
      <Text as="span" color="pos.fg" fontWeight="600" aria-hidden>
        ✓
      </Text>
      <span>{children}</span>
    </Box>
  );
}
