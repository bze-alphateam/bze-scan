"use client";

import { Badge, Box, Button, Grid, Heading, HStack, Input, Link, SimpleGrid, Stack, Tabs, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useMemo, useState } from "react";
import { LuExternalLink, LuSearch } from "react-icons/lu";

import { useAllValidators, useParams } from "@/api/hooks";
import type { Validator, ValidatorList, ValidatorsSummary } from "@/api/types";
import { Avatar } from "@/components/ui/avatar";
import { Card } from "@/components/ui/card";
import { TableSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { Tile } from "@/components/ui/tile";
import { formatCount } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import {
  bondCompact,
  filterByMoniker,
  inactiveLabel,
  pctText,
  rateText,
  signedBlocksWindow,
  splitValidators,
  unbondingText,
  uptimeText,
} from "@/render/validators";

/** The four header tiles; each tolerates a null summary field. */
export function SummaryTiles({ summary }: { summary: ValidatorsSummary | null }) {
  const s = summary;
  return (
    <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3" data-testid="summary-tiles">
      <Tile
        label="Staked BZE"
        value={s?.bonded_tokens ? bondCompact(s.bonded_tokens) : null}
        sub={s?.staked_share ? `${pctText(s.staked_share)} of all BZE is staked` : null}
      />
      <Tile
        label="Reward rate"
        value={s?.reward_rate ? `≈ ${pctText(s.reward_rate)}` : null}
        sub={
          <Link asChild>
            <NextLink href="/parameters#inflation">per year before the validator&apos;s commission</NextLink>
          </Link>
        }
      />
      <Tile
        label="Unstaking takes"
        value={unbondingText(s?.unbonding_period)}
        sub={
          <>
            no rewards while unstaking ·{" "}
            <Link asChild>
              <NextLink href="/parameters#staking">see parameters</NextLink>
            </Link>
          </>
        }
      />
      <Tile
        label="Top 5 validators hold"
        value={s?.top5_share ? pctText(s.top5_share) : null}
        sub="a healthier network keeps this low"
      />
    </SimpleGrid>
  );
}

const COLUMNS = { base: "1fr auto", md: "56px minmax(0, 2.2fr) 1.5fr 1fr 1fr 88px" };

function StakeButton({ moniker }: { moniker: string }) {
  return (
    <Button asChild size="sm" variant="outline" minH="44px">
      <a href={externalLinks.staking} target="_blank" rel="noopener noreferrer" aria-label={`Stake with ${moniker}`}>
        Stake
      </a>
    </Button>
  );
}

/** A label shown above a value on phones, where the header row is hidden. */
function PhoneLabel({ children }: { children: string }) {
  return (
    <Text as="span" hideFrom="md" fontSize="xs" color="fg.muted" me="1">
      {children}
    </Text>
  );
}

function ValidatorRow({ v }: { v: Validator }) {
  const href = `/validators/${encodeURIComponent(v.operator_address)}`;
  return (
    <Grid
      role="listitem"
      templateColumns={COLUMNS}
      gap={{ base: "2", md: "4" }}
      alignItems="center"
      px="4"
      py="3"
      minH="56px"
      borderTopWidth="1px"
      borderColor="border.panel"
      data-testid="validator-row"
    >
      <Text hideBelow="md" color="fg.muted" fontSize="sm">
        {v.status === "bonded" ? v.rank ?? "—" : inactiveLabel(v.status)}
      </Text>
      <HStack gap="3" minW="0">
        <Avatar name={v.moniker} />
        <Stack gap="0.5" minW="0">
          <HStack gap="2" minW="0">
            <Link asChild fontWeight="medium" truncate>
              <NextLink href={href}>{v.moniker}</NextLink>
            </Link>
            {v.jailed && (
              <Badge colorPalette="red" variant="subtle" size="sm">
                Jailed
              </Badge>
            )}
          </HStack>
          <Text hideFrom="md" fontSize="xs" color="fg.muted">
            {v.status === "bonded" ? `#${v.rank ?? "—"}` : inactiveLabel(v.status)}
          </Text>
        </Stack>
      </HStack>
      <Box hideFrom="md" gridRow="span 1">
        <StakeButton moniker={v.moniker} />
      </Box>
      <Text fontSize="sm" gridColumn={{ base: "1 / -1", md: "auto" }}>
        <PhoneLabel>Staked</PhoneLabel>
        {bondCompact(v.tokens)}
        {v.voting_power_pct && (
          <Text as="span" color="fg.muted">
            {" "}
            · {pctText(v.voting_power_pct)}
          </Text>
        )}
      </Text>
      <Text fontSize="sm" gridColumn={{ base: "1 / -1", md: "auto" }}>
        <PhoneLabel>Commission</PhoneLabel>
        {rateText(v.commission_rate)}
      </Text>
      <Text fontSize="sm" gridColumn={{ base: "1 / -1", md: "auto" }}>
        <PhoneLabel>Uptime</PhoneLabel>
        {uptimeText(v.uptime)}
      </Text>
      <Box hideBelow="md">
        <StakeButton moniker={v.moniker} />
      </Box>
    </Grid>
  );
}

function ValidatorTable({ items, empty }: { items: Validator[]; empty: string }) {
  return (
    <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" overflow="hidden">
      <Grid
        hideBelow="md"
        templateColumns={COLUMNS}
        gap="4"
        px="4"
        h="10"
        alignItems="center"
        fontSize="xs"
        color="fg.muted"
        textTransform="uppercase"
        letterSpacing="wide"
        aria-hidden
      >
        <Text>#</Text>
        <Text>Validator</Text>
        <Text>Staked with them</Text>
        <Text>Commission</Text>
        <Text>Uptime</Text>
        <span />
      </Grid>
      <Box role="list" aria-label="Validators">
        {items.map((v) => (
          <ValidatorRow key={v.operator_address} v={v} />
        ))}
      </Box>
      {items.length === 0 && (
        <Text px="4" py="6" color="fg.muted" fontSize="sm" borderTopWidth="1px" borderColor="border.panel">
          {empty}
        </Text>
      )}
    </Box>
  );
}

function PickingCard({ seats }: { seats: number | null }) {
  return (
    <Card title="Picking a validator">
      <Stack gap="3" fontSize="sm">
        <Text>
          Any active validator pays the same base reward. Lower commission means more for you, and staking with validators
          outside the top 5 keeps the network harder to attack.
        </Text>
        <Text color="fg.muted">
          Inactive validators are outside the {seats ? formatCount(seats) : "active"} seats or were jailed, and pay no
          rewards.
        </Text>
        <HStack gap="4" wrap="wrap">
          <Link href={externalLinks.staking} target="_blank" rel="noopener noreferrer" minH="44px">
            Stake on the Staking app <LuExternalLink aria-hidden />
          </Link>
          <Link href={externalLinks.docs} target="_blank" rel="noopener noreferrer" minH="44px">
            How staking works, in the docs <LuExternalLink aria-hidden />
          </Link>
        </HStack>
      </Stack>
    </Card>
  );
}

export function ValidatorsSkeleton() {
  return (
    <Stack gap="6" aria-busy data-testid="validators-skeleton">
      <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3">
        {Array.from({ length: 4 }).map((_, i) => (
          <TileSkeleton key={i} />
        ))}
      </SimpleGrid>
      <TableSkeleton rows={10} columns={6} />
    </Stack>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <Card title="The validators could not be loaded">
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

type Tab = "active" | "inactive";

/** The list once loaded: intro, tiles, tabs with the filter, the table and the footnotes. */
export function ValidatorsView({ list }: { list: ValidatorList }) {
  const [tab, setTab] = useState<Tab>("active");
  const [query, setQuery] = useState("");
  const params = useParams();
  const { active, inactive } = useMemo(() => splitValidators(list.items), [list.items]);
  const seats = list.summary?.max_validators ?? null;
  const window = signedBlocksWindow(params.data) ?? 10_000;
  // Each panel filters its own list: the panel leaving keeps its rows while its exit animation runs.
  const activeRows = filterByMoniker(active, query);
  const inactiveRows = filterByMoniker(inactive, query);

  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Text color="fg.muted" maxW="3xl">
        {formatCount(active.length)} validators produce blocks right now
        {seats ? `, out of ${formatCount(seats)} seats` : ""}. Validators run the chain and earn rewards for the people who
        stake BZE with them. They do not hold your BZE: staked coins stay in your account.
      </Text>
      <SummaryTiles summary={list.summary} />
      <Stack gap="3">
        <Tabs.Root value={tab} onValueChange={(e) => setTab(e.value as Tab)} variant="line" lazyMount unmountOnExit>
          <HStack justify="space-between" wrap="wrap" gap="3">
            <Tabs.List>
              <Tabs.Trigger value="active" minH="44px">
                Active · {formatCount(active.length)}
              </Tabs.Trigger>
              <Tabs.Trigger value="inactive" minH="44px">
                Inactive · {formatCount(inactive.length)}
              </Tabs.Trigger>
            </Tabs.List>
            <HStack
              gap="2"
              px="3"
              borderWidth="1px"
              borderColor="border.panel"
              rounded="l2"
              w={{ base: "full", sm: "64" }}
              color="fg.muted"
            >
              <LuSearch aria-hidden />
              <Input
                aria-label="Find a validator"
                placeholder="Find a validator"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                variant="flushed"
                border="0"
                minH="44px"
              />
            </HStack>
          </HStack>
          <Tabs.Content value="active" pt="3">
            <ValidatorTable items={activeRows} empty={query ? "No active validator has that name." : "No active validators."} />
          </Tabs.Content>
          <Tabs.Content value="inactive" pt="3">
            <ValidatorTable
              items={inactiveRows}
              empty={query ? "No inactive validator has that name." : "Every validator is active."}
            />
          </Tabs.Content>
        </Tabs.Root>
        <Text fontSize="sm" color="fg.muted">
          Commission is the share of your rewards the validator keeps. Uptime is the share of the last{" "}
          {formatCount(window)} blocks the validator signed.
        </Text>
      </Stack>
      <PickingCard seats={seats} />
    </Stack>
  );
}

/** `/validators`: the skeleton on the first load, then the list (polled; never a skeleton again). */
export function ValidatorsPage() {
  const { data, error, refetch } = useAllValidators();
  return (
    <Stack gap="6">
      <Heading as="h1" size={{ base: "2xl", md: "3xl" }}>
        Validators
      </Heading>
      {data ? (
        <ValidatorsView list={data} />
      ) : error ? (
        <LoadError onRetry={() => void refetch()} />
      ) : (
        <ValidatorsSkeleton />
      )}
    </Stack>
  );
}
