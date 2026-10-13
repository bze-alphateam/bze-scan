"use client";

import { Box, HStack, IconButton, Input, Skeleton, Text, type StackProps } from "@chakra-ui/react";
import { useRouter } from "next/navigation";
import { useId, useState, type FormEvent, type KeyboardEvent } from "react";
import { LuSearch } from "react-icons/lu";

import { useSearch } from "@/api/hooks";
import type { SearchResult } from "@/api/types";
import { SearchResultRow } from "@/components/search/search-result-row";
import { classifyQuery, orderResults, resultHref } from "@/lib/search";
import { useDebounced } from "@/lib/use-debounced";

interface SearchBoxProps extends StackProps {
  size?: "md" | "lg";
  autoFocus?: boolean;
  /** Called after a search is submitted (closes the phone drawer or box). */
  onSubmitted?: () => void;
}

/** How long typing must pause before the box asks the search route. */
export const SEARCH_DEBOUNCE_MS = 250;

/**
 * The search box. On submit an identifier (height, hash, address) opens its
 * page without a request; other text is searched by name while typing, the
 * matches listed under the box (arrows, Enter, Escape), Enter picking the
 * first one. Without JavaScript the form submits to `/search?q=`.
 */
export function SearchBox({ size = "md", autoFocus, onSubmitted, ...props }: SearchBoxProps) {
  const router = useRouter();
  const id = useId();
  const [q, setQ] = useState("");
  const [focused, setFocused] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const [active, setActive] = useState(-1);

  const kind = classifyQuery(q);
  const account = kind.kind === "go" ? kind.account : undefined;
  const lookup = kind.kind === "search" ? kind.q : (account ?? "");
  const settled = useDebounced(lookup, SEARCH_DEBOUNCE_MS);
  const search = useSearch(settled, focused && lookup !== "");
  const fresh = settled === lookup && search.data != null;

  // A full address is one row, "Open account"; the answer only says whether it was seen.
  const items: SearchResult[] = account
    ? [{ type: "account", id: account, label: "", indexed: fresh ? search.data?.results.find((r) => r.id === account)?.indexed : undefined }]
    : fresh
      ? orderResults(search.data!.results)
      : [];
  const open = focused && !dismissed && lookup !== "";
  const loading = open && !account && !fresh && !search.isError;

  function go(href: string) {
    router.push(href);
    setQ("");
    setActive(-1);
    onSubmitted?.();
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    if (open && items[active]) return go(resultHref(items[active]));
    if (kind.kind === "go") return go(kind.href);
    if (kind.kind === "search") return go(items[0] ? resultHref(items[0]) : `/search?q=${encodeURIComponent(kind.q)}`);
  }

  function keyDown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      setDismissed(true);
      setActive(-1);
    } else if (open && items.length > 0 && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
      e.preventDefault();
      setActive((a) => (e.key === "ArrowDown" ? Math.min(a + 1, items.length - 1) : Math.max(a - 1, -1)));
    }
  }

  const listId = `${id}-results`;
  const optionId = (i: number) => `${id}-result-${i}`;

  return (
    <HStack
      asChild
      gap="2.5"
      w="full"
      h={size === "lg" ? "44px" : "40px"}
      ps="14px"
      pe="6px"
      bg="bg.page"
      borderWidth="1px"
      borderColor="border.panel"
      rounded="l2"
      position="relative"
      _focusWithin={{ borderColor: "accent.fg" }}
      css={{ "&:focus-within .search-go": { color: "accent.fg", borderColor: "accent.fg" } }}
      {...props}
    >
      <form role="search" action="/search" method="get" onSubmit={submit}>
        <Box flex="1" minW="0" alignSelf="stretch" display="flex">
          <Input
            name="q"
            type="search"
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={open}
            aria-controls={listId}
            aria-activedescendant={open && items[active] ? optionId(active) : undefined}
            autoComplete="off"
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setDismissed(false);
              setActive(-1);
            }}
            onFocus={() => setFocused(true)}
            onBlur={() => {
              setFocused(false);
              setActive(-1);
            }}
            onKeyDown={keyDown}
            placeholder="Search by block, transaction hash, address or validator"
            aria-label="Search the explorer"
            autoFocus={autoFocus}
            unstyled
            flex="1"
            minW="0"
            h="full"
            bg="transparent"
            color="fg"
            outline="none"
            _placeholder={{ color: "fg.muted" }}
          />
          {open && (
            <Box
              position="absolute"
              top="calc(100% + 4px)"
              insetX="-1px"
              zIndex="dropdown"
              bg="bg.panel"
              borderWidth="1px"
              borderColor="border.panel"
              rounded="l2"
              shadow="md"
              py="1"
              textAlign="start"
              data-testid="search-dropdown"
            >
              <Box id={listId} role="listbox" aria-label="Search results">
                {items.map((r, i) => (
                  <Box
                    key={`${r.type}:${r.id}`}
                    id={optionId(i)}
                    role="option"
                    aria-selected={i === active}
                    px="3"
                    py="2"
                    minH="44px"
                    display="flex"
                    alignItems="center"
                    cursor="pointer"
                    bg={i === active ? "bg.muted" : undefined}
                    _hover={{ bg: "bg.muted" }}
                    // Keep the focus in the box so the click lands before the blur closes the list.
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => go(resultHref(r))}
                  >
                    <SearchResultRow r={r} open={account != null} />
                  </Box>
                ))}
              </Box>
              {loading && (
                <Box px="3" py="3" aria-hidden data-testid="search-skeleton">
                  <Skeleton h="4" w="60%" />
                </Box>
              )}
              {fresh && items.length === 0 && (
                <Text px="3" py="3" fontSize="sm" color="fg.muted">
                  No validator or known account matches
                </Text>
              )}
              {search.isError && !account && (
                <Text px="3" py="3" fontSize="sm" color="fg.muted">
                  The search could not be run. Press Enter to try the search page.
                </Text>
              )}
            </Box>
          )}
        </Box>
        <IconButton
          type="submit"
          aria-label="Search"
          className="search-go"
          variant="outline"
          flex="none"
          boxSize="28px"
          minW="28px"
          h="28px"
          rounded="l1"
          color="fg.muted"
          borderColor="border.panel"
          _hover={{ color: "accent.fg", borderColor: "accent.fg", bg: "transparent" }}
        >
          <LuSearch />
        </IconButton>
      </form>
    </HStack>
  );
}
