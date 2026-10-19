"use client";

import { Box, HStack, Heading, Link, Text, type LinkProps } from "@chakra-ui/react";
import NextLink from "next/link";
import type { ReactNode } from "react";

import { ExternalLink } from "@/components/ui/external-link";
import { useBlock, useRawTx, useStatus } from "@/api/hooks";
import type { Tx } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { DetailList, DetailRow } from "@/components/ui/detail-list";
import { JsonBlock } from "@/components/ui/json-block";
import { ListPanel } from "@/components/ui/list";
import { Breadcrumb as Breadcrumbs, DetailMeta, DetailTitle } from "@/components/ui/page-head";
import { JsonSkeleton } from "@/components/ui/skeleton";
import { StatusPill, Tag } from "@/components/ui/status-pill";
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
  return <Breadcrumbs path={[{ label: "Transactions", href: "/txs" }]} current={shortHash(hash)} mono />;
}

/** The full hash, wrapping on phones, with a copy button. */
export function FullHash({ hash }: { hash: string }) {
  return (
    <DetailMeta align="flex-start" gap="2">
      <Text as="span" fontFamily="mono" fontSize="12px" color="fg" overflowWrap="anywhere" minW="0" data-testid="tx-hash">
        {hash.toUpperCase()}
      </Text>
      <CopyButton value={hash.toUpperCase()} label="Copy hash" />
    </DetailMeta>
  );
}

/** The breadcrumb, "Transaction" with `children` beside it (the status) and the full hash. */
export function TxHead({ hash, children, meta }: { hash: string; children?: ReactNode; meta?: ReactNode }) {
  return (
    <Box>
      <Breadcrumb hash={hash} />
      <DetailTitle title="Transaction">{children}</DetailTitle>
      {meta}
      <FullHash hash={hash} />
    </Box>
  );
}

export function TxHeader({ tx }: { tx: Tx }) {
  return (
    <TxHead
      hash={tx.hash}
      meta={
        <DetailMeta data-testid="tx-when">
          <Text as="span">
            <TimeAgo time={tx.time} />, in block{" "}
            <Link asChild fontFamily="mono" color="accent.fg">
              <NextLink href={blockHref(tx.height)}>{formatCount(tx.height)}</NextLink>
            </Link>
          </Text>
        </DetailMeta>
      }
    >
      <StatusPill success={tx.success} />
    </TxHead>
  );
}

/** "Why it failed": the plain-word reason, the chain's log under it when it says more. */
export function FailureCard({ reason, log }: { reason: string; log: string | null }) {
  return (
    <Box mt="6" bg="neg.subtle" borderWidth="1px" borderColor="neg.fg" rounded="l3" px="5" py="4" role="alert">
      <Heading as="h2" fontSize="15px" fontWeight="600" color="neg.fg" mb="1">
        Why it failed
      </Heading>
      <Text fontSize="13px">{reason}</Text>
      {log && log !== reason && (
        <Text mt="2" fontSize="12px" fontFamily="mono" color="fg.muted" wordBreak="break-word">
          {log}
        </Text>
      )}
    </Box>
  );
}

const Muted = ({ children }: { children: ReactNode }) => (
  <Text as="span" color="fg.muted">
    {children}
  </Text>
);

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
    <Muted>
      · {formatCount(n)} confirmation{n === 1 ? "" : "s"}
    </Muted>
  );
}

export function Overview({ tx, ctx }: { tx: Tx; ctx: RenderContext }) {
  return (
    <Card title="Overview" p="0" mt="8" data-testid="tx-overview">
      <DetailList>
        <DetailRow label="Block">
          <Link asChild fontFamily="mono" color="accent.fg">
            <NextLink href={blockHref(tx.height)}>{formatCount(tx.height)}</NextLink>
          </Link>
          <Confirmations height={tx.height} />
        </DetailRow>
        <DetailRow label="Time">
          <TimeAgo time={tx.time} />
          <Muted>· {formatDateTime(tx.time)}</Muted>
        </DetailRow>
        <DetailRow label="Fee">
          <Text as="span" fontFamily="mono">
            {tx.fee.length ? coinsText(tx.fee, ctx) : "none"}
          </Text>
          {tx.fee_payer && (
            <>
              <Muted>· paid by</Muted>
              <AccountChip address={tx.fee_payer} label={ctx.label(tx.fee_payer)} />
            </>
          )}
        </DetailRow>
        <DetailRow label="Memo">{tx.memo ? <Text as="span">{tx.memo}</Text> : <Muted>none</Muted>}</DetailRow>
        <DetailRow label="Signed by">
          {tx.signers.length ? (
            tx.signers.map((s) => <AccountChip key={s} address={s} label={ctx.label(s)} />)
          ) : (
            <Muted>unknown</Muted>
          )}
        </DetailRow>
        <DetailRow label="Position" last>
          <Position height={tx.height} index={tx.tx_index} />
        </DetailRow>
      </DetailList>
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

/** "Open on Ping.pub": on the raw bar from c2 up, under the JSON on phones. */
function PingPubOpen({ hash, ...props }: { hash: string } & Omit<LinkProps, "href" | "children">) {
  return (
    <ExternalLink href={pingPubTxUrl(hash)} fontSize="12px" whiteSpace="nowrap" {...props}>
      Open on Ping.pub
    </ExternalLink>
  );
}

/** The content of "More details": gas, message types, the raw transaction and Ping.pub. */
export function TxDetails({ tx }: { tx: Tx }) {
  return (
    <ListPanel>
      <DetailList>
        <DetailRow label="Gas">
          {tx.gas_used != null ? (
            <>
              <Text as="span" fontFamily="mono">
                {formatCount(tx.gas_used)}
              </Text>
              {tx.gas_wanted != null && (
                <>
                  <Muted>of</Muted>
                  <Text as="span" fontFamily="mono">
                    {formatCount(tx.gas_wanted)}
                  </Text>
                </>
              )}
            </>
          ) : (
            <Muted>unknown</Muted>
          )}
        </DetailRow>
        <DetailRow label="Messages" last>
          {tx.msg_types.map((t, i) => (
            <Tag key={i} bg="transparent" color="fg" borderWidth="1px" borderColor="border.panel" fontFamily="mono" textTransform="none" letterSpacing="0" maxW="full" truncate>
              {t}
            </Tag>
          ))}
        </DetailRow>
      </DetailList>
      <HStack gap="0.5" px="2" borderTopWidth="1px" borderColor="border.panel">
        <Heading
          as="h3"
          px="2.5"
          py="10px"
          fontSize="13px"
          fontWeight="500"
          borderBottomWidth="2px"
          borderColor="accent.fg"
          whiteSpace="nowrap"
        >
          Raw transaction
        </Heading>
        <Box flex="1" />
        <PingPubOpen hash={tx.hash} hideBelow="c2" px="2" />
      </HStack>
      <Box p="4" pt="3" borderTopWidth="1px" borderColor="border.panel">
        <RawTx hash={tx.hash} />
      </Box>
      <PingPubOpen hash={tx.hash} hideFrom="c2" display="inline-block" px="4" pb="4" />
    </ListPanel>
  );
}
