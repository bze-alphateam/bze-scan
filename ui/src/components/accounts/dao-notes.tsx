"use client";

import { Flex, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";

import { useDaosByAdmin } from "@/api/hooks";
import type { Account } from "@/api/types";
import { Callout } from "@/components/ui/callout";
import { daoHref, daoOfLabel } from "@/render/daos";

/** On a DAO's treasury address: says whose it is and links the DAO page. */
export function DaoBanner({ a }: { a: Account }) {
  const dao = daoOfLabel(a.label);
  if (!dao) return null;
  return (
    <Callout tone="acc" mt="5" title={`This is the treasury of ${a.label?.name}`} data-testid="dao-banner">
      A DAO holds its coins at this address and moves them only by its own rules.{" "}
      <Link asChild color="accent.fg">
        <NextLink href={daoHref(dao.id)}>Open the DAO page →</NextLink>
      </Link>
    </Callout>
  );
}

/** "Admin of 2 DAOs: …": the DAOs this address runs; nothing while loading, on a failure or when it runs none. */
export function AdminOfDaos({ address }: { address: string }) {
  const daos = useDaosByAdmin(address);
  const items = daos.data ?? [];
  if (items.length === 0) return null;
  return (
    <Flex gap="1.5" align="center" wrap="wrap" mt="3" fontSize="13px" color="fg.muted" data-testid="admin-of-daos">
      <Text as="span">
        Admin of {items.length} {items.length === 1 ? "DAO" : "DAOs"}:
      </Text>
      {items.map((d, i) => (
        <Text as="span" key={d.id}>
          <Link asChild color="accent.fg">
            <NextLink href={daoHref(d.id)}>{`DAO #${d.id}${d.name ? ` · ${d.name}` : ""}`}</NextLink>
          </Link>
          {i < items.length - 1 ? "," : ""}
        </Text>
      ))}
    </Flex>
  );
}
