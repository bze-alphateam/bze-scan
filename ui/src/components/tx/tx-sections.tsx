"use client";

import { Badge, Box, Grid, HStack, Heading, Link, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { LuExternalLink } from "react-icons/lu";

import { useBlock, useRawTx, useStatus } from "@/api/hooks";
import type { Tx } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { JsonBlock } from "@/components/ui/json-block";
import { JsonSkeleton } from "@/components/ui/skeleton";
import { StatusPill } from "@/components/ui/status-pill";
import { TimeAgo } from "@/components/ui/time-ago";
import { formatCount, formatDateTime, ordinal, shortHash } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { coinsText } from "@/render/messages/common";
import type { RenderContext } from "@/render/messages/types";

const blockHref = (height: number) => `/blocks/${height}`;

export function pingPubTxUrl(hash: string): string {
  return `${externalLinks.pingPub}/tx/${hash.toUpperCase()}`;
}

/** "Transactions / 3F9A…C21E". */
export function Breadcrumb({ hash }: { hash: string }) {
  return (
    <HStack as="nav" aria-label="Breadcrumb" gap="2" fontSize="sm" color="fg.muted">
      <Link asChild color="fg.muted">
        <NextLink href="/txs">Transactions</NextLink>
      </Link>
      <Text as="span" aria-hidden>
        /
      </Text>
      <Text as="span" fontFamily="mono" aria-current="page">
        {shortHash(hash)}
      </Text>
    </HStack>
  );
}

/** The full hash, wrapping on phones, with a copy button. */
export function FullHash({ hash }: { hash: string }) {
  return (
    <HStack gap="1" align="start">
      <Text fontFamily="mono" fontSize="sm" wordBreak="break-all" data-testid="tx-hash">
        {hash.toUpperCase()}
      </Text>
      <CopyButton value={hash.toUpperCase()} label="Copy hash" />
    </HStack>
  );
}

export function TxHeader({ tx }: { tx: Tx }) {
  return (
    <Stack gap="3">
      <Breadcrumb hash={tx.hash} />
      <HStack gap="3" wrap="wrap">
        <Heading as="h1" size={{ base: "2xl", md: "3xl" }}>
          Transaction
        </Heading>
        <StatusPill success={tx.success} />
      </HStack>
      <Text color="fg.muted" fontSize="sm">
        <TimeAgo time={tx.time} />, in block{" "}
        <Link asChild>
          <NextLink href={blockHref(tx.height)}>{formatCount(tx.height)}</NextLink>
        </Link>
      </Text>
      <FullHash hash={tx.hash} />
    </Stack>
  );
}

/** "Why it failed": the plain-word reason, the chain's log under it when it says more. */
export function FailureCard({ reason, log }: { reason: string; log: string | null }) {
  return (
    <Box bg="red.subtle" borderWidth="1px" borderColor="red.muted" rounded="l3" p={{ base: "4", md: "5" }} role="alert">
      <Heading as="h2" size="md" color="red.fg" mb="1">
        Why it failed
      </Heading>
      <Text>{reason}</Text>
      {log && log !== reason && (
        <Text mt="2" fontSize="xs" fontFamily="mono" color="fg.muted" wordBreak="break-word">
          {log}
        </Text>
      )}
    </Box>
  );
}

interface RowProps {
  label: string;
  children: React.ReactNode;
}

function Row({ label, children }: RowProps) {
  return (
    <>
      <Text as="dt" fontSize="sm" color="fg.muted" mt={{ base: "2", sm: "0" }}>
        {label}
      </Text>
      <Box as="dd" fontSize="sm" minW="0">
        {children}
      </Box>
    </>
  );
}

/** "3rd of 7 transactions in the block", once the block is loaded. */
function Position({ height, index }: { height: number; index: number }) {
  const { data } = useBlock(height);
  if (!data) return <Text as="span">{ordinal(index + 1)} in the block</Text>;
  return (
    <Text as="span">
      {ordinal(index + 1)} of {formatCount(data.tx_count)} transaction{data.tx_count === 1 ? "" : "s"} in the block
    </Text>
  );
}

function Confirmations({ height }: { height: number }) {
  const { data } = useStatus();
  const tip = data?.live_fill.db_height;
  if (tip == null || tip < height) return null;
  const n = tip - height;
  return (
    <Text as="span" color="fg.muted">
      {" "}
      · {formatCount(n)} confirmation{n === 1 ? "" : "s"}
    </Text>
  );
}

export function Overview({ tx, ctx }: { tx: Tx; ctx: RenderContext }) {
  return (
    <Card title="Overview">
      <Grid as="dl" templateColumns={{ base: "1fr", sm: "minmax(8rem, max-content) 1fr" }} columnGap="6" rowGap={{ base: "1", sm: "3" }}>
        <Row label="Block">
          <Link asChild>
            <NextLink href={blockHref(tx.height)}>{formatCount(tx.height)}</NextLink>
          </Link>
          <Confirmations height={tx.height} />
        </Row>
        <Row label="Time">
          <TimeAgo time={tx.time} />
          <Text as="span" color="fg.muted">
            {" "}
            · {formatDateTime(tx.time)}
          </Text>
        </Row>
        <Row label="Fee">
          <HStack gap="2" wrap="wrap">
            <Text as="span">{tx.fee.length ? coinsText(tx.fee, ctx) : "none"}</Text>
            {tx.fee_payer && (
              <>
                <Text as="span" color="fg.muted">
                  · paid by
                </Text>
                <AccountChip address={tx.fee_payer} label={ctx.label(tx.fee_payer)} />
              </>
            )}
          </HStack>
        </Row>
        <Row label="Memo">
          {tx.memo ? (
            <Text as="span" wordBreak="break-word">
              {tx.memo}
            </Text>
          ) : (
            <Text as="span" color="fg.muted">
              none
            </Text>
          )}
        </Row>
        <Row label="Signed by">
          <HStack gap="2" wrap="wrap">
            {tx.signers.length ? (
              tx.signers.map((s) => <AccountChip key={s} address={s} label={ctx.label(s)} />)
            ) : (
              <Text as="span" color="fg.muted">
                unknown
              </Text>
            )}
          </HStack>
        </Row>
        <Row label="Position">
          <Position height={tx.height} index={tx.tx_index} />
        </Row>
      </Grid>
    </Card>
  );
}

function RawTx({ hash }: { hash: string }) {
  const { data, isError } = useRawTx(hash);
  if (isError) {
    return (
      <Text fontSize="sm" color="fg.muted">
        The raw transaction could not be loaded from the archive nodes. Try again later, or open it on Ping.pub.
      </Text>
    );
  }
  return data ? <JsonBlock value={data} /> : <JsonSkeleton />;
}

/** The content of "More details": gas, message types, the raw transaction and Ping.pub. */
export function TxDetails({ tx }: { tx: Tx }) {
  return (
    <Stack gap="5" bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" p={{ base: "4", md: "5" }}>
      <Grid as="dl" templateColumns={{ base: "1fr", sm: "minmax(8rem, max-content) 1fr" }} columnGap="6" rowGap={{ base: "1", sm: "3" }}>
        <Row label="Gas">
          {tx.gas_used != null && tx.gas_wanted != null
            ? `${formatCount(tx.gas_used)} of ${formatCount(tx.gas_wanted)}`
            : tx.gas_used != null
              ? formatCount(tx.gas_used)
              : "unknown"}
        </Row>
        <Row label="Messages">
          <HStack gap="2" wrap="wrap">
            {tx.msg_types.map((t, i) => (
              <Badge key={i} variant="outline" fontFamily="mono" textTransform="none" maxW="full" truncate>
                {t}
              </Badge>
            ))}
          </HStack>
        </Row>
      </Grid>
      <Stack gap="2">
        <Heading as="h3" size="sm">
          Raw transaction
        </Heading>
        <RawTx hash={tx.hash} />
      </Stack>
      <Link href={pingPubTxUrl(tx.hash)} target="_blank" rel="noopener noreferrer" fontSize="sm" alignSelf="start">
        Open on Ping.pub <LuExternalLink aria-hidden />
      </Link>
    </Stack>
  );
}
