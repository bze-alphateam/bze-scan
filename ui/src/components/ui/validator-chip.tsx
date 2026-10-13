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

/** A validator as a chip: a 20px avatar and its moniker, linking to its page, the operator address on hover. */
export function ValidatorChip({ operator, moniker, ...props }: ValidatorChipProps) {
  const name = moniker || shortAddress(operator);
  const logo = useValidatorDirectory().data?.get(operator)?.logo_url;
  return (
    <Link asChild maxW="full" color="accent.fg">
      <NextLink href={`/validators/${encodeURIComponent(operator)}`} title={operator} aria-label={name}>
        <HStack as="span" gap="2" display="inline-flex" minW="0" maxW="full" {...props}>
          <Avatar name={moniker || operator} src={logo} size="5" />
          <Text as="span" truncate fontFamily={moniker ? undefined : "mono"}>
            {name}
          </Text>
        </HStack>
      </NextLink>
    </Link>
  );
}
