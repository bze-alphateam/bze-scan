"use client";

import { Box, Button, Grid, Heading, HStack, Link, SimpleGrid, Skeleton, SkeletonCircle, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import type { ReactNode } from "react";
import { LuExternalLink } from "react-icons/lu";

import { isNotFound } from "@/api/client";
import { useAllValidators, useParams, useValidator } from "@/api/hooks";
import type { ValidatorDetail, ValidatorsSummary } from "@/api/types";
import { Avatar } from "@/components/ui/avatar";
import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { JsonBlock } from "@/components/ui/json-block";
import { AddressLink } from "@/components/ui/links";
import { MoreDetails } from "@/components/ui/more-details";
import { CardSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { Tile } from "@/components/ui/tile";
import { TimeAgo } from "@/components/ui/time-ago";
import { formatAmount, formatCount, formatDate } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import {
  bondCompact,
  bondFull,
  cleanRecord,
  historyItems,
  pctText,
  pointsText,
  proposalFrequency,
  rateText,
  rewardRateAfterCommission,
  signedBlocksWindow,
  splitValidators,
  statusLine,
  uptimeDetail,
  uptimeText,
  voteOption,
} from "@/render/validators";

export function pingPubValidatorUrl(operator: string): string {
  return `${externalLinks.pingPub}/staking/${operator}`;
}

/** `linktr.ee/jasonsopko` from `https://linktr.ee/jasonsopko`; null for anything that is not an http(s) URL. */
export function websiteLink(website: string | null): { href: string; text: string } | null {
  if (!website) return null;
  const raw = /^https?:\/\//i.test(website) ? website : `https://${website}`;
  try {
    const url = new URL(raw);
    if (url.protocol !== "https:" && url.protocol !== "http:") return null;
    const text = (url.host + url.pathname).replace(/^www\./, "").replace(/\/$/, "");
    return { href: url.toString(), text };
  } catch {
    return null;
  }
}

function Breadcrumb({ name }: { name: string }) {
  return (
    <HStack as="nav" aria-label="Breadcrumb" gap="2" fontSize="sm" color="fg.muted" minW="0">
      <Link asChild color="fg.muted">
        <NextLink href="/validators">Validators</NextLink>
      </Link>
      <Text as="span" aria-hidden>
        /
      </Text>
      <Text as="span" aria-current="page" truncate>
        {name}
      </Text>
    </HStack>
  );
}

function Header({ v, activeCount }: { v: ValidatorDetail; activeCount: number | null }) {
  const site = websiteLink(v.website);
  const status = statusLine(v, activeCount);
  const active = v.status === "bonded" && !v.jailed && !v.tombstoned;
  return (
    <Stack gap="3">
      <Breadcrumb name={v.moniker} />
      <HStack gap="4" align="start" wrap="wrap" justify="space-between">
        <HStack gap="3" minW="0">
          <Avatar name={v.moniker} size="12" fontSize="md" />
          <Stack gap="1" minW="0">
            <Heading as="h1" size={{ base: "2xl", md: "3xl" }} wordBreak="break-word">
              {v.moniker}
            </Heading>
            <HStack gap="2" wrap="wrap" fontSize="sm">
              <Text color={active ? "green.fg" : v.jailed || v.tombstoned ? "red.fg" : "fg.muted"} fontWeight="medium" data-testid="validator-status">
                {status}
              </Text>
              {site && (
                <Link href={site.href} target="_blank" rel="noopener noreferrer nofollow">
                  {site.text} <LuExternalLink aria-hidden />
                </Link>
              )}
            </HStack>
            <Text fontSize="sm" color="fg.muted">
              {[
                v.first_seen_time ? `Validating since ${formatDate(v.first_seen_time)}` : null,
                v.delegator_count != null
                  ? `${formatCount(v.delegator_count)} ${v.delegator_count === 1 ? "account stakes" : "accounts stake"} here`
                  : null,
              ]
                .filter(Boolean)
                .join(" · ")}
            </Text>
          </Stack>
        </HStack>
        <Button asChild colorPalette="blue" minH="44px">
          <a href={externalLinks.staking} target="_blank" rel="noopener noreferrer">
            Stake with this validator <LuExternalLink aria-hidden />
          </a>
        </Button>
      </HStack>
    </Stack>
  );
}

function Tiles({ v, summary, window }: { v: ValidatorDetail; summary: ValidatorsSummary | null; window: number | null }) {
  const yours = rewardRateAfterCommission(summary?.reward_rate, v.commission_rate);
  const signedWindow = v.signed_blocks_window ?? window;
  return (
    <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3" data-testid="validator-tiles">
      <Tile
        label="Staked with them"
        value={bondCompact(v.tokens)}
        sub={v.voting_power_pct ? `${pctText(v.voting_power_pct)} of all staked BZE` : "outside the active set"}
      />
      <Tile
        label="Commission"
        value={rateText(v.commission_rate)}
        sub={`can change by at most ${pointsText(v.commission_max_change_rate)} a day, never above ${rateText(v.commission_max_rate)}`}
      />
      <Tile
        label="Your reward rate here"
        value={yours ? `≈ ${pctText(yours)}` : null}
        sub={
          summary?.reward_rate
            ? `${pctText(summary.reward_rate)} base, minus the ${rateText(v.commission_rate)} commission`
            : "the base rate minus the commission"
        }
      />
      <Tile label="Uptime" value={uptimeText(v.uptime)} sub={uptimeDetail(v.missed_blocks, signedWindow)} />
    </SimpleGrid>
  );
}

/** A label/value row of a details list; stacks on phones. */
function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Grid templateColumns={{ base: "1fr", md: "180px minmax(0, 1fr)" }} gap={{ base: "0.5", md: "4" }} py="2">
      <Text fontSize="sm" color="fg.muted">
        {label}
      </Text>
      <Box fontSize="sm" minW="0" wordBreak="break-word">
        {children}
      </Box>
    </Grid>
  );
}

function About({ v }: { v: ValidatorDetail }) {
  return (
    <Card title="About">
      <Stack gap="0">
        {v.details && (
          <Text fontSize="sm" mb="3" whiteSpace="pre-line">
            {v.details}
          </Text>
        )}
        <Row label="Validator address">
          <HStack gap="1" align="start">
            <Text fontFamily="mono" wordBreak="break-all">
              {v.operator_address}
            </Text>
            <CopyButton value={v.operator_address} label="Copy validator address" />
          </HStack>
        </Row>
        <Row label="Owner account">
          <AddressLink address={v.account_address} />
          <Text as="span" color="fg.muted">
            {" "}
            · self-stake and commission land here
          </Text>
        </Row>
        <Row label="Self-stake">
          {v.self_delegation != null ? bondFull(v.self_delegation) : "—"}
          <Text as="span" color="fg.muted">
            {" "}
            · the validator&apos;s own money at risk
          </Text>
        </Row>
        {v.security_contact && <Row label="Contact">{v.security_contact}</Row>}
        {v.identity && <Row label="Identity">{v.identity}</Row>}
      </Stack>
    </Card>
  );
}

function RecentBlocks({ v }: { v: ValidatorDetail }) {
  const blocks = v.recent_blocks;
  return (
    <Card title="Recent blocks proposed" meta={proposalFrequency(v.voting_power_pct)}>
      {blocks.length === 0 ? (
        <Text fontSize="sm" color="fg.muted">
          No blocks proposed yet.
        </Text>
      ) : (
        <Stack gap="0" role="list">
          {blocks.map((b, i) => (
            <HStack
              key={b.height}
              role="listitem"
              justify="space-between"
              gap="3"
              py="2"
              minH="44px"
              borderTopWidth={i === 0 ? "0" : "1px"}
              borderColor="border.panel"
              fontSize="sm"
              data-testid="recent-block"
            >
              <HStack gap="2" wrap="wrap">
                <Link asChild>
                  <NextLink href={`/blocks/${b.height}`}>Block {formatCount(b.height)}</NextLink>
                </Link>
                <TimeAgo time={b.time} color="fg.muted" />
              </HStack>
              <Text color="fg.muted">
                {b.tx_count === 0 ? "no transactions" : `${formatCount(b.tx_count)} ${b.tx_count === 1 ? "transaction" : "transactions"}`}
              </Text>
            </HStack>
          ))}
        </Stack>
      )}
    </Card>
  );
}

function History({ v }: { v: ValidatorDetail }) {
  const items = historyItems(v.events);
  const clean = cleanRecord(v);
  return (
    <Card title="History" meta="changes worth knowing about">
      <Stack gap="3" fontSize="sm">
        {items.length > 0 && (
          <Stack gap="0" role="list" aria-label="Validator history">
            {items.map((h, i) => (
              <Stack
                key={h.key}
                role="listitem"
                gap="0.5"
                py="2"
                borderTopWidth={i === 0 ? "0" : "1px"}
                borderColor="border.panel"
                data-testid="history-item"
              >
                <Text>{h.text}</Text>
                <HStack gap="1" color="fg.muted" fontSize="xs">
                  <Text as="span">{h.date}</Text>
                  {h.txHash && (
                    <>
                      <Text as="span">·</Text>
                      <Link asChild color="fg.muted">
                        <NextLink href={`/txs/${h.txHash.toUpperCase()}`}>see transaction</NextLink>
                      </Link>
                    </>
                  )}
                </HStack>
              </Stack>
            ))}
          </Stack>
        )}
        {clean && <Text color="green.fg">{clean}</Text>}
        <Box data-testid="validator-votes">
          {v.votes.length === 0 ? (
            <Text color="fg.muted">{v.moniker} has not voted yet.</Text>
          ) : (
            <Text>
              Last votes:{" "}
              {v.votes.map((vote, i) => (
                <Text as="span" key={`${vote.proposal_id}-${vote.height}`}>
                  {i > 0 && ", "}
                  {voteOption(vote)} on{" "}
                  <Link asChild>
                    <NextLink href={`/proposals/${vote.proposal_id}`}>
                      #{vote.proposal_id}
                      {vote.title ? ` ${vote.title}` : ""}
                    </NextLink>
                  </Link>
                </Text>
              ))}
            </Text>
          )}
        </Box>
      </Stack>
    </Card>
  );
}

function Details({ v }: { v: ValidatorDetail }) {
  return (
    <Stack gap="4">
      <Card>
        <Stack gap="0">
          <Row label="Consensus key">
            <Text fontFamily="mono" wordBreak="break-all">
              {v.consensus_pubkey ?? "—"}
            </Text>
          </Row>
          <Row label="Commission limits">
            max {rateText(v.commission_max_rate)} · max change {pointsText(v.commission_max_change_rate)} per day
          </Row>
          <Row label="Minimum self-stake">
            {v.min_self_delegation != null ? formatAmount(v.min_self_delegation, 0, "ubze") : "—"}
          </Row>
          <Row label="Delegator shares">{formatAmount(v.delegator_shares, 6)}</Row>
        </Stack>
      </Card>
      <Stack gap="2">
        <Heading as="h3" size="sm">
          Raw validator
        </Heading>
        <JsonBlock value={v} />
      </Stack>
      <Link href={pingPubValidatorUrl(v.operator_address)} target="_blank" rel="noopener noreferrer" fontSize="sm" alignSelf="start">
        Open on Ping.pub <LuExternalLink aria-hidden />
      </Link>
    </Stack>
  );
}

export function ValidatorSkeleton() {
  return (
    <Stack gap="6" aria-busy data-testid="validator-skeleton">
      <Stack gap="3">
        <Skeleton h="4" w="40" />
        <HStack gap="3">
          <SkeletonCircle size="12" />
          <Stack gap="2" flex="1">
            <Skeleton h="8" w="64" maxW="full" />
            <Skeleton h="4" w="56" maxW="full" />
          </Stack>
        </HStack>
      </Stack>
      <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3">
        {Array.from({ length: 4 }).map((_, i) => (
          <TileSkeleton key={i} />
        ))}
      </SimpleGrid>
      <CardSkeleton lines={5} />
      <CardSkeleton lines={5} />
      <CardSkeleton lines={4} />
    </Stack>
  );
}

/** A found validator. */
export function ValidatorView({ v }: { v: ValidatorDetail }) {
  const all = useAllValidators();
  const params = useParams();
  const activeCount = all.data ? splitValidators(all.data.items).active.length : null;
  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Header v={v} activeCount={activeCount} />
      <Tiles v={v} summary={all.data?.summary ?? null} window={signedBlocksWindow(params.data)} />
      <About v={v} />
      <Grid templateColumns={{ base: "1fr", lg: "1fr 1fr" }} gap="6" alignItems="start">
        <RecentBlocks v={v} />
        <History v={v} />
      </Grid>
      <MoreDetails hint="consensus key, limits, raw JSON">
        <Details v={v} />
      </MoreDetails>
    </Stack>
  );
}

function Message({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card title={title}>
      <Stack gap="3" fontSize="sm" color="fg.muted">
        {children}
      </Stack>
    </Card>
  );
}

/** `/validators/[operator]`: the skeleton, then the validator (polled), not found on a 404, or the error state. */
export function ValidatorPage({ operator }: { operator: string }) {
  const { data, error, refetch } = useValidator(operator);
  if (data) return <ValidatorView v={data} />;
  if (isNotFound(error)) {
    return (
      <Message title="Validator not found">
        <Text>The explorer does not know this validator yet. Check the address, or look it up on Ping.pub.</Text>
        <PingPubLink operator={operator} />
      </Message>
    );
  }
  if (error) {
    return (
      <Card title="This validator could not be loaded">
        <HStack justify="space-between" wrap="wrap" gap="3">
          <Text color="fg.muted" fontSize="sm">
            The explorer did not answer. Try again in a moment.
          </Text>
          <Button colorPalette="blue" minH="44px" onClick={() => void refetch()}>
            Try again
          </Button>
        </HStack>
      </Card>
    );
  }
  return <ValidatorSkeleton />;
}

function PingPubLink({ operator }: { operator: string }) {
  return (
    <Link href={pingPubValidatorUrl(operator)} target="_blank" rel="noopener noreferrer" alignSelf="start">
      Look it up on Ping.pub <LuExternalLink aria-hidden />
    </Link>
  );
}
