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

// The page box's divider from the arrows: from c2 up only, since on phones it wraps under them.
// (Custom breakpoints are not in Chakra's generated types, so these objects are hoisted.)
const JUMP_INDENT = { base: "0", c2: "3" };
const JUMP_LINE = { base: "0", c2: "1px" };

/**
 * One pager square: a link (`href`), a button (`onClick`), or dimmed when
 * there is nowhere to go (null): a span standing for the link, a disabled button.
 */
function Pg({ href, onClick, label, children }: { href?: string | null; onClick?: (() => void) | null; label: string; children: ReactNode }) {
  if (onClick !== undefined) {
    return (
      <Box
        as="button"
        {...{ type: "button", disabled: onClick == null }}
        {...square}
        cursor="pointer"
        _enabled={{ _hover: hoverSquare }}
        _disabled={{ color: "fg.muted", opacity: 0.5, cursor: "default", borderColor: "border.panel" }}
        aria-label={label}
        onClick={onClick ?? undefined}
      >
        {children}
      </Box>
    );
  }
  if (href == null) {
    return (
      <Box as="span" {...square} color="fg.muted" opacity={0.5} aria-label={label} aria-disabled="true" role="link">
        {children}
      </Box>
    );
  }
  return (
    <Link asChild {...square} _hover={{ textDecoration: "none", ...hoverSquare }}>
      <NextLink href={href} aria-label={label}>
        {children}
      </NextLink>
    </Link>
  );
}

const hoverSquare = { borderColor: "accent.fg", color: "accent.fg" } as const;

/** The current page's square. */
function Current({ page }: { page: number }) {
  return (
    <Box as="span" {...square} bg="accent.subtle" borderColor="accent.fg" color="accent.fg" aria-current="page" data-testid="pager-current">
      {page}
    </Box>
  );
}

/** The bar itself: `children` (the range, a pill) on the left, the pagination `nav` on the right. */
function PagerBar({ children, nav }: { children: ReactNode; nav: ReactNode }) {
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
        {nav}
      </HStack>
    </Flex>
  );
}

const pill = {
  display: "inline-flex",
  alignItems: "center",
  gap: "6px",
  h: "24px",
  px: "10px",
  rounded: "12px",
  bg: "accent.subtle",
  color: "accent.fg",
  fontSize: "12px",
  fontWeight: "500",
  _before: { content: '""', boxSize: "6px", rounded: "full", bg: "accent.fg" },
} as const;

/**
 * "N new blocks · back to latest": an accent pill with a dot, under a pinned
 * list whose source moved on. A link with `href`, a button with `onClick`.
 */
export function PagerPill({ href, onClick, children, testId }: { href?: string; onClick?: () => void; children: ReactNode; testId?: string }) {
  if (onClick) {
    return (
      <Box as="button" {...{ type: "button" }} {...pill} cursor="pointer" _hover={{ filter: "brightness(1.15)" }} onClick={onClick} data-testid={testId}>
        {children}
      </Box>
    );
  }
  return (
    <Link asChild {...pill} _hover={{ textDecoration: "none", filter: "brightness(1.15)" }}>
      <NextLink href={href ?? "#"} data-testid={testId}>
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
    <PagerBar
      nav={
        <>
          <Pg href={atStart ? null : firstHref} label="First page">
            «
          </Pg>
          <Pg href={atStart ? null : href(page - 1)} label="Previous page">
            ‹
          </Pg>
          <Current page={page} />
          <Pg href={atEnd ? null : href(page + 1)} label="Next page">
            ›
          </Pg>
          <Pg href={atEnd || last == null ? null : href(last)} label="Last page">
            »
          </Pg>
          <HStack
            as="form"
            gap="6px"
            ms={JUMP_INDENT}
            ps={JUMP_INDENT}
            borderLeftWidth={JUMP_LINE}
            borderColor="border.panel"
            data-testid="pager-jump"
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
        </>
      }
    >
      {children}
    </PagerBar>
  );
}

interface PrevNextPagerProps {
  /** The left side: what the list shows, and a `PagerPill` when there is news. */
  children: ReactNode;
  page: number;
  /** Back to the live first page; null on it. */
  onFirst: (() => void) | null;
  onPrev: (() => void) | null;
  /** The page after the last row; null when there is none. */
  onNext: (() => void) | null;
}

/**
 * The bar under a list that pages by cursor only: « ‹ page › with buttons,
 * no page box and no last page (pages can't be counted or jumped to).
 */
export function PrevNextPager({ children, page, onFirst, onPrev, onNext }: PrevNextPagerProps) {
  return (
    <PagerBar
      nav={
        <>
          <Pg onClick={onFirst} label="Newest page">
            «
          </Pg>
          <Pg onClick={onPrev} label="Previous page">
            ‹
          </Pg>
          <Current page={page} />
          <Pg onClick={onNext} label="Next page">
            ›
          </Pg>
        </>
      }
    >
      {children}
    </PagerBar>
  );
}
