"use client";

import { Box, Button, Flex, HStack, Link, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useState, type ReactNode } from "react";
import { LuCircleCheck, LuTriangleAlert } from "react-icons/lu";

import { useEmergency, useEmergencyActions, useLiveHalt, useStats } from "@/api/hooks";
import type { Emergency, EmergencyAction, EmergencyDenom } from "@/api/types";
import { Callout } from "@/components/ui/callout";
import { Chip } from "@/components/ui/chip";
import { ListPanel, ListRow } from "@/components/ui/list";
import { PageTitle } from "@/components/ui/page-head";
import { CardSkeleton, TableSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { TimeAgo } from "@/components/ui/time-ago";
import { useListRenderContext } from "@/components/txs/list-context";
import { formatCount } from "@/lib/format";
import {
  actionHref,
  actionLinkText,
  actionMessage,
  channelText,
  emergencyTokenName,
  guardianDao,
  haltCountdown,
  haltReached,
  hasScheduledHalt,
  IBC_PAGE,
  measureCount,
  ownTitle,
} from "@/render/emergency";
import { humanise } from "@/render/messages/common";
import { NestedList } from "@/render/messages/v830-common";
import { tokenHref } from "@/render/tokens";

type Tab = "status" | "history";

/** The emergency status page: what is switched off or halted right now, and the history of the switches. */
export function EmergencyPage() {
  const [tab, setTab] = useState<Tab>("status");
  return (
    <>
      <PageTitle title="Emergency status" intro="Whether any emergency measure is in force on BZE, what it means for you, and the switches pulled and undone." />
      <HStack gap="2" mb="5" role="tablist" aria-label="Emergency status views">
        {(["status", "history"] as const).map((t) => (
          <Button
            key={t}
            role="tab"
            aria-selected={tab === t}
            size="sm"
            variant={tab === t ? "solid" : "outline"}
            onClick={() => setTab(t)}
            data-testid={`emergency-tab-${t}`}
          >
            {t === "status" ? "Status" : "History"}
          </Button>
        ))}
      </HStack>
      {tab === "status" ? <StatusTab /> : <HistoryTab />}
    </>
  );
}

function StatusTab() {
  const emergency = useEmergency();
  const stats = useStats();
  if (emergency.isPending) return <CardSkeleton lines={4} />;
  if (emergency.isError) {
    return (
      <StateBox tone="error" icon={<LuTriangleAlert size={20} />} title="The emergency state could not be read" data-testid="emergency-read-failed">
        The explorer could not read the emergency switches from the chain just now. Nothing is hidden by this: try again in a minute.
      </StateBox>
    );
  }
  const e = emergency.data;
  if (!e.supported) {
    return (
      <StateBox tone="info" icon={<LuCircleCheck size={20} />} title="Emergency switches arrive with chain v8.3.0" data-testid="emergency-unsupported">
        The chain this explorer reads has no emergency switches yet. This page fills in once it runs v8.3.0.
      </StateBox>
    );
  }
  return (
    <Stack gap="5">
      <StatusCard e={e} />
      <Sections e={e} avgBlockMs={stats.data?.avg_block_time_ms ?? null} />
    </Stack>
  );
}

function StatusCard({ e }: { e: Emergency }) {
  const n = measureCount(e);
  const dao = guardianDao(e.guardian);
  return (
    <Callout
      tone={e.active ? "warn" : "ok"}
      title={e.active ? `${plural(n, "emergency measure is", "emergency measures are")} active` : "No emergency measures are active"}
      data-testid="emergency-status-card"
    >
      <Stack gap="1">
        <Text>
          Emergency key:{" "}
          {e.guardian ? (
            dao ? (
              <>
                <Chip variant="acc">DAO</Chip>{" "}
                <Link asChild color="accent.fg">
                  <NextLink href={`/daos/${dao.id}`}>
                    DAO #{dao.id}
                    {dao.name ? ` · ${dao.name}` : ""}
                  </NextLink>
                </Link>
              </>
            ) : (
              <Link asChild color="accent.fg">
                <NextLink href={`/accounts/${e.guardian.address}`} title={e.guardian.address}>
                  {e.guardian.label ?? e.guardian.address}
                </NextLink>
              </Link>
            )
          ) : (
            "governance"
          )}
          . Checked at block {formatCount(e.height)}.
        </Text>
      </Stack>
    </Callout>
  );
}

const plural = (n: number, one: string, many: string) => `${formatCount(n)} ${n === 1 ? one : many}`;

function Section({ title, children, testId }: { title: string; children: ReactNode; testId: string }) {
  return (
    <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" borderLeftWidth="3px" borderLeftColor="warn.fg" rounded="l3" p="5" data-testid={testId}>
      <Text as="h2" fontWeight="600" fontSize="15px">
        {title}
      </Text>
      <Stack gap="2" mt="2" fontSize="13px" lineHeight="1.55">
        {children}
      </Stack>
    </Box>
  );
}

function TokenLinks({ denoms }: { denoms: EmergencyDenom[] }) {
  return (
    <Flex gap="2" wrap="wrap">
      {denoms.map((d) => (
        <Link key={d.denom} asChild color="accent.fg" fontWeight="500">
          <NextLink href={tokenHref(d.denom)} title={d.denom}>
            {emergencyTokenName(d)}
          </NextLink>
        </Link>
      ))}
    </Flex>
  );
}

function Sections({ e, avgBlockMs }: { e: Emergency; avgBlockMs: number | null }) {
  const normal: string[] = [];
  const liveHalt = useLiveHalt().data;
  const stopped = haltReached(e, liveHalt?.halted === true && liveHalt.kind === "emergency");
  const countdown = haltCountdown(e, avgBlockMs);
  const halt = hasScheduledHalt(e) ? e.plan! : null;
  const sections: ReactNode[] = [];

  if (e.halted_denoms.length) {
    sections.push(
      <Section key="halted" title="Trading halted" testId="emergency-halted">
        <TokenLinks denoms={e.halted_denoms} />
        <Text color="fg.muted">You cannot trade these on the DEX until governance lifts the halt.</Text>
      </Section>,
    );
  } else normal.push("Trading");
  if (e.blocked_denoms.length) {
    sections.push(
      <Section key="blocked" title="Blocked from crossing chains" testId="emergency-blocked">
        <TokenLinks denoms={e.blocked_denoms} />
        <Text color="fg.muted">
          These tokens cannot be sent to or received from other chains. See{" "}
          <Link asChild color="accent.fg">
            <NextLink href={IBC_PAGE}>cross-chain transfers</NextLink>
          </Link>
          .
        </Text>
      </Section>,
    );
  } else normal.push("Token transfers between chains");
  if (e.closed_channels.length) {
    sections.push(
      <Section key="channels" title="Channels closed" testId="emergency-channels">
        <Stack as="ul" gap="1" listStyleType="none" m="0" p="0">
          {e.closed_channels.map((c) => (
            <Text as="li" key={c.channel_id}>
              {channelText(c)}
            </Text>
          ))}
        </Stack>
        <Text color="fg.muted">
          Transfers in both directions are refused; incoming ones are refunded by the sending chain. See{" "}
          <Link asChild color="accent.fg">
            <NextLink href={IBC_PAGE}>cross-chain transfers</NextLink>
          </Link>
          .
        </Text>
      </Section>,
    );
  } else normal.push("Cross-chain channels");
  if (e.disabled_messages.length) {
    sections.push(
      <Section key="messages" title="Actions switched off" testId="emergency-messages">
        <Stack as="ul" gap="1" listStyleType="none" m="0" p="0">
          {e.disabled_messages.map((u) => (
            <Text as="li" key={u} title={u}>
              {humanise(u)}
            </Text>
          ))}
        </Stack>
        <Text color="fg.muted">Transactions with these actions fail.</Text>
      </Section>,
    );
  } else normal.push("Every kind of action");
  if (e.automation_paused) {
    sections.push(
      <Section key="automation" title="Fee conversion and burning paused" testId="emergency-automation">
        <Text color="fg.muted">Collected fees are not converted and burned until governance or the emergency key resumes it. Nothing else is affected.</Text>
      </Section>,
    );
  } else normal.push("Fee conversion and burning");
  if (halt) {
    sections.push(
      <Section
        key="halt"
        title={stopped ? `The chain is stopped at block ${formatCount(halt.height)}` : `Chain halt scheduled at block ${formatCount(halt.height)}`}
        testId="emergency-halt"
      >
        {halt.info && <Text>Reason: {halt.info}</Text>}
        <Text color="fg.muted">
          {stopped ? "No new blocks are produced until validators restart the chain." : countdown ? `The chain stops ${countdown}.` : "The chain stops at that block."}
        </Text>
      </Section>,
    );
  } else normal.push("The chain");

  return (
    <Stack gap="4">
      {sections}
      {normal.length > 0 && (
        <Text color="fg.muted" fontSize="13px" data-testid="emergency-normal">
          {e.active ? "Everything else is normal" : "Everything is normal"}: {normal.join(", ")}.
        </Text>
      )}
    </Stack>
  );
}

function HistoryTab() {
  const q = useEmergencyActions();
  const ctx = useListRenderContext();
  if (q.isPending) return <TableSkeleton rows={6} columns={3} />;
  if (q.isError) {
    return (
      <StateBox tone="error" icon={<LuTriangleAlert size={20} />} title="The emergency history could not be read" data-testid="emergency-history-failed">
        Try again in a minute.
      </StateBox>
    );
  }
  const rows = q.data.pages.flatMap((p) => p.items);
  if (rows.length === 0) {
    return (
      <StateBox icon={<LuCircleCheck size={20} />} title="No emergency action yet" data-testid="emergency-history-empty">
        Nothing has been switched off, halted or closed on this chain.
      </StateBox>
    );
  }
  return (
    <Stack gap="4">
      <ListPanel data-testid="emergency-history">
        {rows.map((a, i) => (
          <HistoryRow key={`${a.height}-${i}`} a={a} ctx={ctx} />
        ))}
      </ListPanel>
      {q.hasNextPage && (
        <Button alignSelf="center" size="sm" variant="outline" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
          Show older
        </Button>
      )}
    </Stack>
  );
}

function HistoryRow({ a, ctx }: { a: EmergencyAction; ctx: ReturnType<typeof useListRenderContext> }) {
  const [open, setOpen] = useState(false);
  const msg = actionMessage(a);
  const title = msg ? ctx.rendererFor(msg.type_url).title(msg, ctx) : ownTitle(a);
  const guardian = a.by === "guardian";
  return (
    <ListRow templateColumns="minmax(0, 1fr)" data-testid="emergency-action">
      <Flex gap="3" align="center" wrap="wrap">
        <Text fontWeight="500" flex="1" minW="0">
          {title}
        </Text>
        <Chip variant={guardian ? "warn" : "acc"} data-testid="emergency-by">
          {guardian ? "by guardian" : "by governance"}
        </Chip>
      </Flex>
      <Flex gap="3" align="center" wrap="wrap" fontSize="12px" color="fg.muted">
        <TimeAgo time={a.time} />
        <Link asChild color="accent.fg">
          <NextLink href={actionHref(a)}>{actionLinkText(a)}</NextLink>
        </Link>
        {msg && (
          <Button size="2xs" variant="ghost" onClick={() => setOpen(!open)} aria-expanded={open}>
            {open ? "Hide details" : "Details"}
          </Button>
        )}
      </Flex>
      {open && msg && (
        <Box pt="2">
          <NestedList list={[msg]} ctx={ctx} />
        </Box>
      )}
    </ListRow>
  );
}
