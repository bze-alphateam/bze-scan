"use client";

import { HStack, Link, type StackProps } from "@chakra-ui/react";
import NextLink from "next/link";

import type { Label } from "@/api/types";
import { shortAddress, shortHash } from "@/lib/format";

import { Chip } from "./chip";
import { CopyButton } from "./copy-button";

/** The explorer page of an address: a validator's or an account's. */
export function addressHref(address: string): string {
  const path = address.startsWith("bzevaloper1") ? "validators" : "accounts";
  return `/${path}/${encodeURIComponent(address)}`;
}

interface AddressLinkProps extends StackProps {
  address: string;
  label?: Label | null;
  /** Hide the copy button (dense table cells). */
  noCopy?: boolean;
}

/**
 * A short address linking to its page, with a copy button. A module account
 * shows as a dashed chip "Name · module" with a lock; another labelled account shows its
 * label next to the short address.
 */
export function AddressLink({ address, label, noCopy, ...props }: AddressLinkProps) {
  const href = addressHref(address);
  const isModule = label?.kind === "module";
  return (
    <HStack as="span" gap="1" display="inline-flex" minW="0" {...props}>
      {isModule ? (
        <Link asChild>
          <NextLink href={href} title={address}>
            <Chip variant="modc" fontSize="12px">
              {label.name} · module
            </Chip>
          </NextLink>
        </Link>
      ) : (
        <>
          {label && (
            <Chip>{label.name}</Chip>
          )}
          <Link asChild fontFamily="mono" fontSize="sm">
            <NextLink href={href} title={address}>
              {shortAddress(address)}
            </NextLink>
          </Link>
        </>
      )}
      {!noCopy && <CopyButton value={address} label="Copy address" />}
    </HStack>
  );
}

interface HashLinkProps extends StackProps {
  hash: string;
  /** Defaults to the transaction page. */
  href?: string;
  noCopy?: boolean;
}

/** `3F9A…C21E` linking to its transaction (or `href`), with a copy button. */
export function HashLink({ hash, href, noCopy, fontSize = "sm", ...props }: HashLinkProps) {
  return (
    <HStack as="span" gap="1" display="inline-flex" {...props}>
      <Link asChild fontFamily="mono" fontSize={fontSize}>
        <NextLink href={href ?? `/txs/${encodeURIComponent(hash.toUpperCase())}`} title={hash.toUpperCase()}>
          {shortHash(hash)}
        </NextLink>
      </Link>
      {!noCopy && <CopyButton value={hash.toUpperCase()} label="Copy hash" />}
    </HStack>
  );
}
