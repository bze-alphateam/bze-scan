"use client";

import { Box, Button } from "@chakra-ui/react";
import { useId, useState, type ReactNode } from "react";
import { LuChevronDown } from "react-icons/lu";

interface MoreDetailsProps {
  /** What the block holds: "raw JSON, gas, message types". */
  hint: string;
  children: ReactNode;
}

/**
 * The collapsed block that ends every detail page. Its content mounts on the
 * first open (so raw JSON loads only when asked for) and stays mounted; it
 * only adds content below, the page never rearranges.
 */
export function MoreDetails({ hint, children }: MoreDetailsProps) {
  const [open, setOpen] = useState(false);
  const [mounted, setMounted] = useState(false);
  const id = useId();

  return (
    <Box>
      <Button
        variant="plain"
        size="sm"
        minH="44px"
        px="0"
        gap="1.5"
        color="accent.fg"
        fontSize="13px"
        fontWeight="500"
        // A long hint wraps on phones instead of widening the page.
        h="auto"
        py="2"
        maxW="full"
        whiteSpace="normal"
        textAlign="start"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => {
          setMounted(true);
          setOpen((o) => !o);
        }}
      >
        More details · {hint}
        <Box as="span" transition="transform 0.15s" transform={open ? "rotate(180deg)" : undefined} display="inline-flex">
          <LuChevronDown />
        </Box>
      </Button>
      <Box id={id} hidden={!open} mt="2.5">
        {mounted && children}
      </Box>
    </Box>
  );
}
