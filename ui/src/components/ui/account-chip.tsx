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
 * An account as a chip: an avatar and its label or short address, linking to
 * its page. A module account's border is dashed.
 */
export function AccountChip({ address, label, ...props }: AccountChipProps) {
  const name = label?.name ?? shortAddress(address);
  const isModule = label?.kind === "module";
  return (
    <Link asChild _hover={{ textDecoration: "none", bg: "bg.muted" }} rounded="full" maxW="full">
      <NextLink href={addressHref(address)} title={address} aria-label={isModule ? `${name} · module` : name}>
        <HStack
          as="span"
          gap="1.5"
          display="inline-flex"
          minW="0"
          maxW="full"
          ps="0.5"
          pe="2.5"
          py="0.5"
          borderWidth="1px"
          borderStyle={isModule ? "dashed" : "solid"}
          borderColor="border.panel"
          rounded="full"
          data-module={isModule || undefined}
          {...props}
        >
          <Avatar name={label?.name ?? address} size="5" fontSize="2xs" />
          <Text as="span" fontSize="sm" truncate fontFamily={label ? undefined : "mono"}>
            {name}
          </Text>
        </HStack>
      </NextLink>
    </Link>
  );
}
