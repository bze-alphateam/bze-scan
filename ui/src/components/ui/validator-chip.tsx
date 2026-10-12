"use client";

import { HStack, Link, Text, type StackProps } from "@chakra-ui/react";
import NextLink from "next/link";

import { useValidatorDirectory } from "@/api/hooks";
import { shortAddress } from "@/lib/format";

import { Avatar } from "./avatar";

interface ValidatorChipProps extends StackProps {
  operator: string;
  /** Null while unknown: the short operator address stands in. */
  moniker?: string | null;
}

/** A validator as a chip: an avatar and its moniker, linking to its page. */
export function ValidatorChip({ operator, moniker, ...props }: ValidatorChipProps) {
  const name = moniker || shortAddress(operator);
  const logo = useValidatorDirectory().data?.get(operator)?.logo_url;
  return (
    <Link asChild _hover={{ textDecoration: "none", bg: "bg.muted" }} rounded="full" maxW="full">
      <NextLink href={`/validators/${encodeURIComponent(operator)}`} title={operator} aria-label={name}>
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
          borderColor="border.panel"
          rounded="full"
          {...props}
        >
          <Avatar name={moniker || operator} src={logo} size="5" fontSize="2xs" />
          <Text as="span" fontSize="sm" truncate fontFamily={moniker ? undefined : "mono"}>
            {name}
          </Text>
        </HStack>
      </NextLink>
    </Link>
  );
}
