"use client";

import { HStack, Link, Text, type StackProps } from "@chakra-ui/react";
import NextLink from "next/link";

import type { Label } from "@/api/types";
import { shortAddress } from "@/lib/format";

import { Avatar } from "./avatar";
import { addressHref } from "./links";

interface AccountChipProps extends StackProps {
  address: string;
  label?: Label | null;
}

/**
 * An account as a chip: a 20px avatar and its label or short address, linking
 * to its page, the full address on hover. A module account's avatar is dashed.
 */
export function AccountChip({ address, label, ...props }: AccountChipProps) {
  const name = label?.name ?? shortAddress(address);
  const isModule = label?.kind === "module";
  return (
    <Link asChild maxW="full" color="accent.fg">
      <NextLink href={addressHref(address)} title={address} aria-label={isModule ? `${name} · module` : name}>
        <HStack as="span" gap="2" display="inline-flex" minW="0" maxW="full" data-module={isModule || undefined} {...props}>
          <Avatar name={label?.name ?? address} size="5" variant={isModule ? "module" : "default"} />
          <Text as="span" truncate fontFamily={label ? undefined : "mono"}>
            {name}
          </Text>
        </HStack>
      </NextLink>
    </Link>
  );
}
