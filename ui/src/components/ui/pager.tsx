"use client";

import { Box, Flex, HStack, Input, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useRouter } from "next/navigation";
import { useId, useState, type ReactNode } from "react";
import { LuArrowRight } from "react-icons/lu";

const square = {
  display: "inline-grid",
  placeItems: "center",
  minW: "30px",
  h: "30px",
  px: "2",
  borderWidth: "1px",
  borderColor: "border.panel",
  rounded: "l1",
  fontFamily: "mono",
  fontSize: "12px",
  color: "fg",
} as const;

/** One pager square: a link, or a dimmed span when there is nowhere to go. */
function Pg({ href, label, children }: { href: string | null; label: string; children: ReactNode }) {
  if (href == null) {
    return (
      <Box as="span" {...square} color="fg.muted" opacity={0.5} aria-label={label} aria-disabled="true" role="link">
        {children}
      </Box>
    );
  }
  return (
    <Link asChild {...square} _hover={{ textDecoration: "none", borderColor: "accent.fg", color: "accent.fg" }}>
      <NextLink href={href} aria-label={label}>
        {children}
      </NextLink>
    </Link>
  );
}

/** "N new blocks · back to latest": an accent pill with a dot, under a pinned list whose source moved on. */
export function PagerPill({ href, children, testId }: { href: string; children: ReactNode; testId?: string }) {
  return (
    <Link
      asChild
      display="inline-flex"
      alignItems="center"
      gap="6px"
      h="24px"
      px="10px"
      rounded="12px"
      bg="accent.subtle"
      color="accent.fg"
      fontSize="12px"
      fontWeight="500"
      _hover={{ textDecoration: "none", filter: "brightness(1.15)" }}
      _before={{ content: '""', boxSize: "6px", rounded: "full", bg: "accent.fg" }}
    >
      <NextLink href={href} data-testid={testId}>
        {children}
      </NextLink>
    </Link>
  );
}

interface PagerProps {
  /** The left side: the range shown, and a `PagerPill` when there is news. */
  children: ReactNode;
  page: number;
  /** The last page; null while unknown (» is then disabled and › follows `hasNext`). */
  last: number | null;
  hasNext: boolean;
  /** Where page n lives. */
  href: (page: number) => string;
  /** Where « goes: the first page, or the live list. */
  firstHref: string;
}

/**
 * The bar under a numbered list: the range on the left; « ‹ page › » and a
 * "Page [#]" box on the right. A typed page is clamped to 1…last.
 */
export function Pager({ children, page, last, hasNext, href, firstHref }: PagerProps) {
  const router = useRouter();
  const [typed, setTyped] = useState("");
  const id = useId();
  const atStart = page <= 1;
  const atEnd = last != null ? page >= last : !hasNext;
  const go = (n: number) => router.push(n === 1 && atStart ? firstHref : href(n));

  return (
    <Flex
      justify="space-between"
      align="center"
      gap="3"
      wrap="wrap"
      px="4"
      py="10px"
      borderTopWidth="1px"
      borderColor="border.panel"
      fontSize="12px"
      data-testid="pager"
    >
      <HStack gap="3" wrap="wrap">
        {children}
      </HStack>
      <HStack as="nav" aria-label="Pagination" gap="1" wrap="wrap">
        <Pg href={atStart ? null : firstHref} label="First page">
          «
        </Pg>
        <Pg href={atStart ? null : href(page - 1)} label="Previous page">
          ‹
        </Pg>
        <Box
          as="span"
          {...square}
          bg="accent.subtle"
          borderColor="accent.fg"
          color="accent.fg"
          aria-current="page"
          data-testid="pager-current"
        >
          {page}
        </Box>
        <Pg href={atEnd ? null : href(page + 1)} label="Next page">
          ›
        </Pg>
        <Pg href={atEnd || last == null ? null : href(last)} label="Last page">
          »
        </Pg>
        <HStack
          as="form"
          gap="6px"
          ms="3"
          ps="3"
          borderLeftWidth="1px"
          borderColor="border.panel"
          onSubmit={(e) => {
            e.preventDefault();
            const n = Number.parseInt(typed, 10);
            if (!Number.isFinite(n)) return;
            setTyped("");
            go(Math.max(1, last != null ? Math.min(n, last) : n));
          }}
        >
          <Text asChild color="fg.muted">
            <label htmlFor={id}>Page</label>
          </Text>
          <Input
            id={id}
            value={typed}
            onChange={(e) => setTyped(e.target.value.replace(/\D/g, ""))}
            inputMode="numeric"
            placeholder="#"
            w="72px"
            h="30px"
            px="2"
            fontFamily="mono"
            fontSize="12px"
            bg="bg.page"
            borderColor="border.panel"
            rounded="l1"
            _focus={{ borderColor: "accent.fg", outline: "none", boxShadow: "none" }}
          />
          <Box
            as="button"
            {...{ type: "submit" }}
            {...square}
            px="0"
            w="30px"
            color="fg.muted"
            cursor="pointer"
            _hover={{ borderColor: "accent.fg", color: "accent.fg" }}
            aria-label="Go to page"
          >
            <LuArrowRight size={14} aria-hidden />
          </Box>
        </HStack>
      </HStack>
    </Flex>
  );
}
