"use client";

import { Button, HStack, Input, type StackProps } from "@chakra-ui/react";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { LuSearch } from "react-icons/lu";

interface SearchBoxProps extends StackProps {
  size?: "md" | "lg";
  autoFocus?: boolean;
  /** Called after a search is submitted (closes the phone drawer or box). */
  onSubmitted?: () => void;
}

/** The search box: submits to `/search?q=`. */
export function SearchBox({ size = "md", autoFocus, onSubmitted, ...props }: SearchBoxProps) {
  const router = useRouter();
  const [q, setQ] = useState("");

  function submit(e: FormEvent) {
    e.preventDefault();
    const query = q.trim();
    if (!query) return;
    router.push(`/search?q=${encodeURIComponent(query)}`);
    onSubmitted?.();
  }

  return (
    <HStack as="form" role="search" onSubmit={submit} gap="2" w="full" {...props}>
      <Input
        name="q"
        type="search"
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search by block, transaction hash, address or validator"
        aria-label="Search the explorer"
        autoFocus={autoFocus}
        size={size}
        minH="44px"
        bg="bg.panel"
        borderColor="border.panel"
      />
      <Button type="submit" size={size} minH="44px" colorPalette="blue" flexShrink={0}>
        <LuSearch />
        Search
      </Button>
    </HStack>
  );
}
