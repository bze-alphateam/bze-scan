"use client";

import { Box, Button, Flex, Skeleton, Spinner, Stack, Text } from "@chakra-ui/react";
import { useMemo } from "react";
import { LuCircleHelp, LuTriangleAlert } from "react-icons/lu";

import { ExternalLink } from "@/components/ui/external-link";
import { useTokenDirectory, useTransaction, useValidatorDirectory } from "@/api/hooks";
import { AppLink } from "@/components/layout/app-link";
import type { Transfer, Tx } from "@/api/types";
import { StepList } from "@/components/funds-flow/step-list";
import { isSimple } from "@/components/funds-flow/net";
import { ActionCard } from "@/components/ui/action-card";
import { ListPanel } from "@/components/ui/list";
import { MoreDetails } from "@/components/ui/more-details";
import { SectionHead } from "@/components/ui/page-head";
import { CardSkeleton, ListSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { failureReason } from "@/render/errors";
import { createRenderContext } from "@/render/messages/registry";
import type { RenderContext, RenderMessage } from "@/render/messages/types";
import { appDestinations } from "@/render/tx-apps";

import { FailureCard, Overview, pingPubTxUrl, TxDetails, TxHead, TxHeader } from "./tx-sections";

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
    <Box>
      <TxHead hash={hash} />
      {state === "looking" ? (
        <StateBox
          mt="6"
          tone="info"
          icon={<Spinner size="sm" />}
          title="Looking for this transaction…"
          role="status"
          data-testid="tx-looking"
        >
          If you just sent it, it appears within seconds.
        </StateBox>
      ) : (
        <StateBox
          mt="6"
          icon={<LuCircleHelp size={20} />}
          title="Not found"
          actions={
            <ExternalLink href={pingPubTxUrl(hash)}>Look it up on Ping.pub</ExternalLink>
          }
          data-testid="tx-not_found"
        >
          If it was sent recently, check the hash; transactions older than the explorer&apos;s history are not indexed.
        </StateBox>
      )}
    </Box>
  );
}

function LoadError({ hash, onRetry }: { hash: string; onRetry: () => void }) {
  return (
    <Box>
      <TxHead hash={hash} />
      <StateBox
        mt="6"
        tone="error"
        icon={<LuTriangleAlert size={20} />}
        title="This transaction could not be loaded"
        actions={
          <Button size="sm" colorPalette="blue" onClick={onRetry}>
            Try again
          </Button>
        }
        data-testid="tx-error"
      >
        The explorer did not answer. Try again in a moment.
      </StateBox>
    </Box>
  );
}

const asRenderMessage = (m: Tx["messages"][number]): RenderMessage => ({ ...m, msg_index: m.msg_index });

/** The action cards: each message through its renderer. */
function Actions({ tx, ctx }: { tx: Tx; ctx: RenderContext }) {
  if (tx.messages.length === 0) {
    return (
      <Text color="fg.muted" fontSize="13px">
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

/** "Vote on Staking": where the action can be done again, one link per app; failed transactions too. */
function AppLinks({ tx }: { tx: Tx }) {
  const apps = appDestinations(tx.messages.map(asRenderMessage));
  if (apps.length === 0) return null;
  return (
    <Flex mt="3" gap="4" wrap="wrap" data-testid="tx-apps">
      {apps.map((d) => (
        <Text key={d.app} as="span" fontSize="sm" color="fg.muted" whiteSpace="nowrap">
          {d.verb}{" "}
          <AppLink app={d.app} href={d.href}>
            {d.name}
          </AppLink>
        </Text>
      ))}
    </Flex>
  );
}

function Section({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <Box as="section" mt="8" aria-labelledby={id}>
      <SectionHead id={id} title={title} />
      {children}
    </Box>
  );
}

/** A found transaction. */
export function TxView({ tx }: { tx: Tx }) {
  const tokens = useTokenDirectory();
  const validators = useValidatorDirectory();
  const ctx = useMemo(
    () =>
      createRenderContext({
        transfers: tx.transfers,
        tokens: tokens.data,
        validators: validators.data,
        dex: tx.dex,
        ibcTransfers: tx.ibc_transfers,
      }),
    [tx.transfers, tokens.data, validators.data, tx.dex, tx.ibc_transfers],
  );

  // A failed transaction only paid its fee.
  const transfers = tx.success ? tx.transfers : tx.transfers.filter((t) => t.msg_index === null);
  const simple = isSimple(tx.messages.length, transfers.length);
  const signer = tx.signers[0] ?? tx.fee_payer;
  // A DEX transaction always shows its steps and what the signer gained and gave.
  const collapsed = simple && !tx.messages.some((m) => m.type_url.startsWith("/bze.tradebin."));

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
    <Box>
      <TxHeader tx={tx} />
      <AppLinks tx={tx} />
      {failure && <FailureCard reason={failure} log={tx.error_log} />}
      <Section id="tx-actions-title" title="What happened">
        <Actions tx={tx} ctx={ctx} />
      </Section>
      <Section id="tx-flow-title" title="Where the funds went">
        <ListPanel>
          <StepList transfers={transfers} reasonOf={reasonOf} collapsed={collapsed} netOf={collapsed ? null : signer} />
        </ListPanel>
      </Section>
      <Overview tx={tx} ctx={ctx} />
      <Box mt="4">
        <MoreDetails hint="gas, message types, raw JSON">
          <TxDetails tx={tx} />
        </MoreDetails>
      </Box>
    </Box>
  );
}

/** `/txs/[hash]`: the skeleton, then the transaction, the pending state on a 404, or the error state. */
export function TxPage({ hash }: { hash: string }) {
  const { data, error, pending, refetch } = useTransaction(hash);
  if (data) return <TxView tx={data} />;
  if (pending) return <Pending hash={hash} state={pending} />;
  if (error) return <LoadError hash={hash} onRetry={() => void refetch()} />;
  return <TxSkeleton />;
}
