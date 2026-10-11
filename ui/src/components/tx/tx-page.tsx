"use client";

import { Box, Button, Heading, HStack, Link, Skeleton, Stack, Text } from "@chakra-ui/react";
import { useMemo } from "react";
import { LuExternalLink } from "react-icons/lu";

import { useTokenDirectory, useTransaction, useValidatorDirectory } from "@/api/hooks";
import type { Transfer, Tx } from "@/api/types";
import { StepList } from "@/components/funds-flow/step-list";
import { isSimple } from "@/components/funds-flow/net";
import { ActionCard } from "@/components/ui/action-card";
import { Card } from "@/components/ui/card";
import { MoreDetails } from "@/components/ui/more-details";
import { CardSkeleton, ListSkeleton } from "@/components/ui/skeleton";
import { failureReason } from "@/render/errors";
import { createRenderContext } from "@/render/messages/registry";
import type { RenderContext, RenderMessage } from "@/render/messages/types";

import { Breadcrumb, FailureCard, FullHash, Overview, pingPubTxUrl, TxDetails, TxHeader } from "./tx-sections";

/** The page's skeleton: header lines, one action, two steps, the overview and the More details bar. */
export function TxSkeleton() {
  return (
    <Stack gap="6" aria-busy data-testid="tx-skeleton">
      <Stack gap="3">
        <Skeleton h="4" w="40" />
        <Skeleton h="9" w="64" />
        <Skeleton h="4" w="56" />
        <Skeleton h="4" w="full" maxW="xl" />
      </Stack>
      <CardSkeleton lines={2} />
      <ListSkeleton rows={2} />
      <CardSkeleton lines={6} />
      <Skeleton h="11" w="56" />
    </Stack>
  );
}

function Pending({ hash, state }: { hash: string; state: "looking" | "not_found" }) {
  return (
    <Stack gap="6">
      <Stack gap="3">
        <Breadcrumb hash={hash} />
        <Heading as="h1" size={{ base: "2xl", md: "3xl" }}>
          Transaction
        </Heading>
        <FullHash hash={hash} />
      </Stack>
      <Card data-testid={`tx-${state}`}>
        {state === "looking" ? (
          <Stack gap="1" role="status">
            <Text fontWeight="semibold">Looking for this transaction…</Text>
            <Text color="fg.muted" fontSize="sm">
              If you just sent it, it appears within seconds.
            </Text>
          </Stack>
        ) : (
          <Stack gap="3">
            <Text fontWeight="semibold">Not found.</Text>
            <Text color="fg.muted" fontSize="sm">
              If it was sent recently, check the hash; transactions older than the explorer&apos;s history are not
              indexed.
            </Text>
            <Link href={pingPubTxUrl(hash)} target="_blank" rel="noopener noreferrer" fontSize="sm" alignSelf="start">
              Look it up on Ping.pub <LuExternalLink aria-hidden />
            </Link>
          </Stack>
        )}
      </Card>
    </Stack>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <Card title="This transaction could not be loaded">
      <HStack justify="space-between" wrap="wrap" gap="3">
        <Text color="fg.muted" fontSize="sm">
          The explorer did not answer. Try again in a moment.
        </Text>
        <Button colorPalette="blue" minH="44px" onClick={onRetry}>
          Try again
        </Button>
      </HStack>
    </Card>
  );
}

const asRenderMessage = (m: Tx["messages"][number]): RenderMessage => ({ ...m, msg_index: m.msg_index });

/** The action cards: each message through its renderer. */
function Actions({ tx, ctx }: { tx: Tx; ctx: RenderContext }) {
  if (tx.messages.length === 0) {
    return (
      <Text color="fg.muted" fontSize="sm">
        This transaction has no actions.
      </Text>
    );
  }
  return (
    <Stack gap="3">
      {tx.messages.map((m, i) => {
        const msg = asRenderMessage(m);
        const r = ctx.rendererFor(msg.type_url);
        return (
          <ActionCard key={m.msg_index} index={i + 1} category={r.category} title={r.title(msg, ctx)} notExecuted={!tx.success}>
            <r.Body msg={msg} ctx={ctx} />
          </ActionCard>
        );
      })}
    </Stack>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Stack as="section" gap="3">
      <Heading as="h2" size="lg">
        {title}
      </Heading>
      {children}
    </Stack>
  );
}

/** A found transaction. */
export function TxView({ tx }: { tx: Tx }) {
  const tokens = useTokenDirectory();
  const validators = useValidatorDirectory();
  const ctx = useMemo(
    () => createRenderContext({ transfers: tx.transfers, tokens: tokens.data, validators: validators.data }),
    [tx.transfers, tokens.data, validators.data],
  );

  // A failed transaction only paid its fee.
  const transfers = tx.success ? tx.transfers : tx.transfers.filter((t) => t.msg_index === null);
  const simple = isSimple(tx.messages.length, transfers.length);
  const signer = tx.signers[0] ?? tx.fee_payer;

  const reasonOf = (t: Transfer) => {
    const m = tx.messages.find((x) => x.msg_index === t.msg_index);
    if (!m) return "transaction fee";
    const msg = asRenderMessage(m);
    const r = ctx.rendererFor(msg.type_url);
    return r.reasonFor?.(t, msg, ctx) ?? r.title(msg, ctx);
  };

  const failure = tx.success
    ? null
    : failureReason(tx, (denom) => ctx.denom(denom).symbol, tx.fee[0]?.denom ?? null);

  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <TxHeader tx={tx} />
      {failure && <FailureCard reason={failure} log={tx.error_log} />}
      <Section title="What happened">
        <Actions tx={tx} ctx={ctx} />
      </Section>
      <Section title="Where the funds went">
        <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" px={{ base: "4", md: "5" }} py="3">
          <StepList transfers={transfers} reasonOf={reasonOf} collapsed={simple} netOf={simple ? null : signer} />
        </Box>
      </Section>
      <Overview tx={tx} ctx={ctx} />
      <MoreDetails hint="gas, message types, raw JSON">
        <TxDetails tx={tx} />
      </MoreDetails>
    </Stack>
  );
}

/** `/txs/[hash]`: the skeleton, then the transaction, the pending state on a 404, or the error state. */
export function TxPage({ hash }: { hash: string }) {
  const { data, error, pending, refetch } = useTransaction(hash);
  if (data) return <TxView tx={data} />;
  if (pending) return <Pending hash={hash} state={pending} />;
  if (error) return <LoadError onRetry={() => void refetch()} />;
  return <TxSkeleton />;
}
