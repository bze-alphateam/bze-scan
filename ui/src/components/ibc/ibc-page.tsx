"use client";

import { Box, Button, Flex, HStack, Link, Popover, Portal, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { LuCircleHelp, LuTriangleAlert } from "react-icons/lu";

import { useDenoms, useIbcChannels, useRateLimits } from "@/api/hooks";
import type { IbcChannel, RateLimit, RateLimitList } from "@/api/types";
import { Avatar } from "@/components/ui/avatar";
import { Callout } from "@/components/ui/callout";
import { Card } from "@/components/ui/card";
import { Chip, type ChipVariant } from "@/components/ui/chip";
import { PageTitle } from "@/components/ui/page-head";
import { CardSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { LIMITS_EXPLAINER, limitsByChannel, limitSymbol } from "@/render/rate-limits";
import { tokenHref } from "@/render/tokens";

import { LimitBars } from "./limit-bars";
import { useNow } from "./use-now";

const STATE: Record<string, { text: string; look: ChipVariant }> = {
  open: { text: "Open", look: "ok" },
  closed: { text: "Closed", look: "bad" },
};

const CLIENT: Record<string, { text: string; look: ChipVariant }> = {
  active: { text: "Client active", look: "ok" },
  expired: { text: "Client expired", look: "bad" },
  frozen: { text: "Client frozen", look: "bad" },
};

/** The "?" next to the limits: what a limit counts and what happens over it. */
function LimitsHelp() {
  return (
    <Popover.Root positioning={{ placement: "bottom-start" }} lazyMount unmountOnExit>
      <Popover.Trigger asChild>
        <button type="button" aria-label="How limits work" data-testid="limits-help">
          <LuCircleHelp size={16} />
        </button>
      </Popover.Trigger>
      <Portal>
        <Popover.Positioner>
          <Popover.Content maxW="sm">
            <Popover.Arrow />
            <Popover.Body>
              <Text fontSize="sm">{LIMITS_EXPLAINER}</Text>
            </Popover.Body>
          </Popover.Content>
        </Popover.Positioner>
      </Portal>
    </Popover.Root>
  );
}

function LimitRow({ l, now, withChannel = false }: { l: RateLimit; now: Date; withChannel?: boolean }) {
  return (
    <Box py="3" borderTopWidth="1px" borderColor="border.panel" data-testid="limit-row">
      <Link asChild fontWeight="500" color="accent.fg" display="inline-block" mb="2">
        <NextLink href={tokenHref(l.denom)}>{withChannel ? `${limitSymbol(l)} · ${l.channel_id}` : limitSymbol(l)}</NextLink>
      </Link>
      <LimitBars limit={l} now={now} />
    </Box>
  );
}

function ChannelCard({ channel, limits, showLimits, now }: { channel: IbcChannel; limits: RateLimit[]; showLimits: boolean; now: Date }) {
  const state = (channel.state && STATE[channel.state]) || { text: channel.state ?? "Unknown", look: "default" as ChipVariant };
  const client = channel.client_status ? CLIENT[channel.client_status] : null;
  const name = channel.counterparty.pretty_name ?? channel.counterparty.chain_id ?? "Unknown chain";
  return (
    <Card p="0" data-testid="channel-card">
      <Flex gap="3" align="center" wrap="wrap" px="4" py="3">
        <Avatar name={name} src={channel.counterparty.logo_url} size="8" />
        <Box minW="0" flex="1">
          <Text fontWeight="500" truncate data-testid="channel-chain">
            {name}
          </Text>
          <Text fontFamily="mono" fontSize="12px" color="fg.muted" data-testid="channel-id">
            {channel.channel_id}
          </Text>
        </Box>
        <HStack gap="1" wrap="wrap">
          <Chip variant={state.look} data-testid="channel-state">
            {state.text}
          </Chip>
          {client && (
            <Chip variant={client.look} data-testid="channel-client">
              {client.text}
            </Chip>
          )}
        </HStack>
      </Flex>
      {showLimits && limits.length > 0 && (
        <Box px="4" pb="1">
          {limits.map((l) => (
            <LimitRow key={l.denom} l={l} now={now} />
          ))}
        </Box>
      )}
      {showLimits && limits.length === 0 && (
        <Text px="4" pb="3" fontSize="13px" color="fg.muted">
          No limits on this route.
        </Text>
      )}
    </Card>
  );
}

function Blocked({ denoms }: { denoms: string[] }) {
  const tokens = useDenoms();
  return (
    <Card title="Blocked from crossing chains" meta="These tokens cannot be sent over IBC at all." data-testid="blocked-denoms">
      <HStack gap="2" wrap="wrap">
        {denoms.map((d) => (
          <Chip key={d} variant="bad" maxW="full" whiteSpace="normal" wordBreak="break-all">
            <Link asChild color="inherit">
              <NextLink href={tokenHref(d)}>{tokens.data?.get(d)?.symbol ?? d}</NextLink>
            </Link>
          </Chip>
        ))}
      </HStack>
    </Card>
  );
}

/** The loaded page: the channels, each with its limits when the chain has them, and the blocked tokens. */
function IbcView({ channels, limits, limitsFailed, retryLimits }: { channels: IbcChannel[]; limits: RateLimitList | undefined; limitsFailed: boolean; retryLimits: () => void }) {
  const now = useNow();
  const supported = limits?.supported === true;
  const byChannel = limitsByChannel(limits?.items ?? []);
  const known = new Set(channels.map((c) => c.channel_id));
  const others = (limits?.items ?? []).filter((l) => !known.has(l.channel_id));
  return (
    <Stack gap="4">
      {limitsFailed && !limits && (
        <Callout tone="warn" title="The limits could not be loaded" data-testid="limits-error">
          The channels below are shown without their limits.{" "}
          <Link asChild color="accent.fg">
            <button type="button" onClick={retryLimits}>
              Try again
            </button>
          </Link>
        </Callout>
      )}
      {limits && !supported && (
        <Callout tone="acc" title="IBC limits arrive with chain v8.3.0" data-testid="limits-unsupported">
          Until then the channels are listed without limits.
        </Callout>
      )}
      {supported && (
        <HStack gap="2" color="fg.muted" fontSize="13px">
          <Text>How the limits work</Text>
          <LimitsHelp />
        </HStack>
      )}
      {channels.length === 0 ? (
        <StateBox icon={<LuCircleHelp size={20} />} title="No IBC channels" data-testid="ibc-empty">
          The explorer has not seen a channel yet.
        </StateBox>
      ) : (
        channels.map((c) => <ChannelCard key={c.channel_id} channel={c} limits={byChannel.get(c.channel_id) ?? []} showLimits={supported} now={now} />)
      )}
      {supported && others.length > 0 && (
        <Card title="Other channels" meta="Limits on channels the explorer has not listed above." data-testid="other-channels" p="0">
          <Box px="4" pb="1">
            {others.map((l) => (
              <LimitRow key={`${l.channel_id}/${l.denom}`} l={l} now={now} withChannel />
            ))}
          </Box>
        </Card>
      )}
      {supported && limits.blacklisted_denoms.length > 0 && <Blocked denoms={limits.blacklisted_denoms} />}
    </Stack>
  );
}

/** `/ibc`: Cross-chain. The skeleton on the first load, then the channels (polled). */
export function IbcPage() {
  const channels = useIbcChannels();
  const limits = useRateLimits();
  return (
    <Box>
      <PageTitle title="Cross-chain" intro="BZE's IBC channels, and how much of each token may still cross them today." />
      {channels.data ? (
        <IbcView channels={channels.data.items} limits={limits.data} limitsFailed={limits.isError} retryLimits={() => void limits.refetch()} />
      ) : channels.error ? (
        <StateBox
          tone="error"
          icon={<LuTriangleAlert size={20} />}
          title="The channels could not be loaded"
          actions={
            <Button size="sm" colorPalette="blue" onClick={() => void channels.refetch()}>
              Try again
            </Button>
          }
          data-testid="ibc-error"
        >
          The explorer did not answer. Try again in a moment.
        </StateBox>
      ) : (
        <Stack gap="4" aria-busy data-testid="ibc-skeleton">
          <CardSkeleton />
          <CardSkeleton />
        </Stack>
      )}
    </Box>
  );
}
