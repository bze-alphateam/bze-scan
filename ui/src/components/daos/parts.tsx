"use client";

import { Box, HStack, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";

import type { DaoSummary, Label } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Avatar } from "@/components/ui/avatar";
import { Chip } from "@/components/ui/chip";
import { daoHref, daoOfLabel, typeExplainer, typeLabel } from "@/render/daos";

/** The DAO's type as a tag; its meaning is the title (hover) and part of the accessible name. */
export function TypeTag({ type }: { type: string }) {
  return (
    <Chip variant="acc" title={typeExplainer(type)} data-testid="dao-type" data-type={type}>
      {typeLabel(type)}
    </Chip>
  );
}

/** A DAO as a chip: `DAO #7 · Name`, linking to its page. */
export function DaoChip({ id, name }: { id: number; name: string }) {
  return (
    <Link asChild maxW="full" color="accent.fg">
      <NextLink href={daoHref(id)} data-testid="dao-chip">
        <HStack as="span" gap="2" display="inline-flex" minW="0" maxW="full">
          <Avatar name={name || `DAO ${id}`} size="5" />
          <Text as="span" truncate>
            DAO #{id}
            {name ? ` · ${name}` : ""}
          </Text>
        </HStack>
      </NextLink>
    </Link>
  );
}

/**
 * Someone who runs or belongs to a DAO: a DAO (by the address book of the DAOs
 * the page knows, else by its `dao` label) shows as a DAO chip, any other
 * account as an account chip.
 */
export function WhoChip({ address, label, daos }: { address: string; label?: Label | null; daos?: Map<string, DaoSummary> }) {
  const known = daos?.get(address);
  if (known) return <DaoChip id={known.id} name={known.name} />;
  const fromLabel = daoOfLabel(label);
  if (fromLabel) return <DaoChip id={fromLabel.id} name={fromLabel.name} />;
  return <AccountChip address={address} label={label} />;
}

/** A small count badge: the proposals a DAO has open. */
export function OpenBadge({ count }: { count: number | undefined }) {
  if (!count) return null;
  return (
    <Box as="span" data-testid="proposals-open">
      <Chip variant="ok">
        {count} open {count === 1 ? "proposal" : "proposals"}
      </Chip>
    </Box>
  );
}
