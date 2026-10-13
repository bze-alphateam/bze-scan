"use client";

import { IconButton, type IconButtonProps } from "@chakra-ui/react";
import { useEffect, useState } from "react";
import { LuCheck, LuCopy } from "react-icons/lu";

interface CopyButtonProps extends Omit<IconButtonProps, "aria-label" | "onClick"> {
  value: string;
  /** What is copied, for the button's label: "Copy address". */
  label?: string;
}

/** A small square button that copies `value` to the clipboard and shows a check for two seconds. */
export function CopyButton({ value, label = "Copy", ...props }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(id);
  }, [copied]);

  return (
    <IconButton
      variant="outline"
      size="xs"
      boxSize="24px"
      minW="24px"
      h="24px"
      rounded="l1"
      color="fg.muted"
      borderColor="border.panel"
      _hover={{ color: "accent.fg", borderColor: "accent.fg", bg: "transparent" }}
      aria-label={copied ? "Copied" : label}
      title={copied ? "Copied" : label}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
        } catch {
          // No clipboard access (insecure context): nothing to show.
        }
      }}
      {...props}
    >
      {copied ? <LuCheck /> : <LuCopy />}
    </IconButton>
  );
}
