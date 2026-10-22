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
// `touch`: 44px squares on phones, the usual 30px from c2 up.
const TOUCH = { minW: { base: "44px", c2: "30px" }, h: { base: "44px", c2: "30px" } };

/**
 * One pager square: a link (`href`), a button (`onClick`), or dimmed when
 * there is nowhere to go (null): a span standing for the link, a disabled button.
 */
function Pg({
  href,
  onClick,
  onFollow,
  label,
  touch,
  children,
}: {
  touch?: boolean;
  href?: string | null;
  onClick?: (() => void) | null;
  /** With `href`: a plain click runs this instead of navigating (a new tab still opens the link). */
  onFollow?: () => void;
  label?: string;
  children: ReactNode;
}) {
  const size = touch ? TOUCH : null;
  if (onClick !== undefined) {
    return (
      <Box
        as="button"
        {...{ type: "button", disabled: onClick == null }}
        {...square}
        {...size}
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
      <Box as="span" {...square} {...size} color="fg.muted" opacity={0.5} aria-label={label} aria-disabled="true" role="link">
        {children}
      </Box>
    );
  }
  return (
    <Link asChild {...square} {...size} _hover={{ textDecoration: "none", ...hoverSquare }}>
      <NextLink
        href={href}
        aria-label={label}
        onClick={
          onFollow &&
          ((e) => {
            if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
            e.preventDefault();
            onFollow();
          })
        }
      >
        {children}
      </NextLink>
    </Link>
  );
}

const hoverSquare = { borderColor: "accent.fg", color: "accent.fg" } as const;

/** The current page's square. */
function Current({ page, touch }: { page: number; touch?: boolean }) {
  return (
    <Box as="span" {...square} {...(touch ? TOUCH : null)} bg="accent.subtle" borderColor="accent.fg" color="accent.fg" aria-current="page" data-testid="pager-current">
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
  /** Number the pages around the current one (1 … 4 5 6 … N) once `last` is known. */
  numbered?: boolean;
  /** Move in the page instead of navigating: the links keep their `href` for a new tab. */
  onGo?: (page: number) => void;
  /** 44px squares on phones. */
  touch?: boolean;
}

/** The pages a numbered pager shows: first, last and the current ±1, a null for each gap. */
function pageWindow(page: number, last: number): (number | null)[] {
  const shown = [...new Set([1, page - 1, page, page + 1, last])].filter((n) => n >= 1 && n <= last).sort((a, b) => a - b);
  return shown.flatMap((n, i) => (i > 0 && n - shown[i - 1] > 1 ? [null, n] : [n]));
}

/**
 * The bar under a numbered list: the range on the left; « ‹ page › » and a
 * "Page [#]" box on the right. A typed page is clamped to 1…last.
 */
export function Pager({ children, page, last, hasNext, href, firstHref, numbered, onGo, touch }: PagerProps) {
  const router = useRouter();
  const [typed, setTyped] = useState("");
  const id = useId();
  const atStart = page <= 1;
  const atEnd = last != null ? page >= last : !hasNext;
  const go = (n: number) => (onGo ? onGo(n) : router.push(n === 1 && atStart ? firstHref : href(n)));
  const follow = (n: number) => (onGo ? () => onGo(n) : undefined);

  return (
    <PagerBar
      nav={
        <>
          <Pg touch={touch} href={atStart ? null : firstHref} onFollow={follow(1)} label="First page">
            «
          </Pg>
          <Pg touch={touch} href={atStart ? null : href(page - 1)} onFollow={follow(page - 1)} label="Previous page">
            ‹
          </Pg>
          {numbered && last != null ? (
            pageWindow(page, last).map((n, i) =>
              n == null ? (
                <Text as="span" key={`gap-${i}`} px="1" color="fg.muted" fontFamily="mono" aria-hidden>
                  …
                </Text>
              ) : n === page ? (
                <Current key={n} page={n} touch={touch} />
              ) : (
                <Pg key={n} touch={touch} href={n === 1 ? firstHref : href(n)} onFollow={follow(n)} label={`Page ${n}`}>
                  {n}
                </Pg>
              ),
            )
          ) : (
            <Current page={page} touch={touch} />
          )}
          <Pg touch={touch} href={atEnd ? null : href(page + 1)} onFollow={follow(page + 1)} label="Next page">
            ›
          </Pg>
          <Pg touch={touch} href={atEnd || last == null ? null : href(last)} onFollow={last != null ? follow(last) : undefined} label="Last page">
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
