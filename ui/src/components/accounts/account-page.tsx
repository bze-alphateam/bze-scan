"use client";

import { Badge, Box, Button, Heading, HStack, Link, SimpleGrid, Skeleton, SkeletonCircle, Stack, Tabs, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useState, type ReactNode } from "react";
import { LuExternalLink } from "react-icons/lu";

import { ApiError, isNotFound } from "@/api/client";
import { useAccount, useAllValidators, useDenoms } from "@/api/hooks";
import type { Account, ActivityCategory } from "@/api/types";
import { Avatar } from "@/components/ui/avatar";
import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { TileSkeleton } from "@/components/ui/skeleton";
import { Tile } from "@/components/ui/tile";
import { formatCount, formatUsd } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { usdValue } from "@/lib/math";
import {
  ACCOUNT_TABS,
  accountTitle,
  activeSince,
  amountOf,
  BOND_DENOM,
  daysText,
  labelChip,
  parseTab,
  tokenRows,
  unstaking,
  type AccountTab,
} from "@/render/account";
import { BOND, bondCompact } from "@/render/validators";

import { ActivityTab } from "./activity-tab";
import { StakingTab } from "./staking-tab";
import { TokensTab } from "./tokens-tab";

function Header({ a }: { a: Account }) {
  const chip = labelChip(a.label);
  const validators = useAllValidators();
  // A validator owner's label is the validator's moniker.
  const owned =
    a.label?.kind === "validator_owner" ? validators.data?.items.find((v) => v.moniker === a.label?.name) : undefined;
  return (
    <Stack gap="3">
      <HStack gap="3" align="start" minW="0">
        <Avatar name={a.address} size="12" fontSize="md" />
        <Stack gap="1" minW="0">
          <HStack gap="2" wrap="wrap">
            <Heading as="h1" size={{ base: "2xl", md: "3xl" }} wordBreak="break-word">
              {a.label?.name ?? "Account"}
            </Heading>
            {chip && a.label?.kind !== "known" && (
              <Badge
                variant="outline"
                size="md"
                rounded="full"
                borderStyle={chip.dashed ? "dashed" : "solid"}
                borderWidth="1px"
                boxShadow="none"
                data-testid="label-chip"
              >
                {owned ? (
                  <Link asChild>
                    <NextLink href={`/validators/${encodeURIComponent(owned.operator_address)}`}>{chip.text}</NextLink>
                  </Link>
                ) : (
                  chip.text
                )}
              </Badge>
            )}
          </HStack>
          <Text fontSize="sm" color="fg.muted" data-testid="account-since">
            {activeSince(a)}
          </Text>
        </Stack>
      </HStack>
      <HStack gap="1" align="start" minW="0">
        <Text fontFamily="mono" fontSize="sm" wordBreak="break-all" data-testid="account-address">
          {a.address}
        </Text>
        <CopyButton value={a.address} label="Copy address" />
      </HStack>
    </Stack>
  );
}

function StakingLink({ children }: { children: ReactNode }) {
  return (
    <Link href={externalLinks.staking} target="_blank" rel="noopener noreferrer">
      {children} <LuExternalLink aria-hidden />
    </Link>
  );
}

function Tiles({ a }: { a: Account }) {
  const denoms = useDenoms();
  if (!a.live.available) {
    return (
      <Stack gap="3">
        <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3" data-testid="account-tiles">
          <Tile label="Available" value={null} />
          <Tile label="Staked" value={null} />
          <Tile label="Rewards to claim" value={null} />
          <Tile label="Unstaking" value={null} />
        </SimpleGrid>
        <Text fontSize="sm" color="fg.muted" data-testid="live-unavailable">
          Live balances are unavailable right now: the node did not answer. Transactions and activity are still here.
        </Text>
      </Stack>
    );
  }
  const available = amountOf(a.balances, BOND_DENOM);
  const usd = usdValue(available, BOND.exponent, denoms.data?.get(BOND_DENOM)?.price_usd);
  const unbonding = unstaking(a.unbonding, new Date());
  const n = a.delegations.length;
  return (
    <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3" data-testid="account-tiles">
      <Tile label="Available" value={bondCompact(available)} sub={usd != null ? formatUsd(usd) : null} />
      <Tile
        label="Staked"
        value={bondCompact(a.total_staked ?? "0")}
        sub={n === 0 ? "not staking" : `with ${formatCount(n)} ${n === 1 ? "validator" : "validators"}`}
      />
      <Tile
        label="Rewards to claim"
        value={bondCompact(amountOf(a.total_rewards, BOND_DENOM))}
        sub={<StakingLink>Claim on Staking</StakingLink>}
      />
      <Tile
        label="Unstaking"
        value={unbonding.nextDays == null ? "none" : bondCompact(unbonding.total)}
        sub={unbonding.nextDays == null ? null : `next unlock in ${daysText(unbonding.nextDays)}`}
      />
    </SimpleGrid>
  );
}

/** The tab strip's labels: the activity count is indexed; the others count only when the live state could be read. */
function tabLabels(a: Account): Record<AccountTab, string> {
  const activity = `Activity · ${formatCount(a.activity_count)}`;
  if (!a.live.available) return { activity, tokens: "Tokens", staking: "Staking" };
  const tokens = tokenRows(a.balances, undefined, "name").length;
  const n = a.delegations.length;
  return {
    activity,
    tokens: `Tokens · ${formatCount(tokens)}`,
    staking: `Staking · ${formatCount(n)} ${n === 1 ? "validator" : "validators"}`,
  };
}

/** Keeps the tab in the URL without a navigation, so a link opens the same tab. */
function writeTab(tab: AccountTab) {
  const url = new URL(window.location.href);
  if (tab === "activity") url.searchParams.delete("tab");
  else url.searchParams.set("tab", tab);
  // The category belongs to the Activity tab.
  url.searchParams.delete("category");
  window.history.replaceState(window.history.state, "", url);
}

/** A loaded account: header, tiles and the tabs. */
export function AccountView({
  a,
  initialTab = "activity",
  initialCategory = null,
}: {
  a: Account;
  initialTab?: AccountTab;
  initialCategory?: ActivityCategory | null;
}) {
  const [tab, setTab] = useState<AccountTab>(initialTab);
  const labels = tabLabels(a);
  return (
    <Stack gap={{ base: "6", md: "8" }}>
      <Header a={a} />
      <Tiles a={a} />
      <Tabs.Root
        value={tab}
        onValueChange={(e) => {
          const next = parseTab(e.value);
          setTab(next);
          writeTab(next);
        }}
        variant="line"
        lazyMount
      >
        <Box overflowX="auto">
          <Tabs.List>
            {ACCOUNT_TABS.map((t) => (
              <Tabs.Trigger key={t} value={t} minH="44px" whiteSpace="nowrap">
                {labels[t]}
              </Tabs.Trigger>
            ))}
          </Tabs.List>
        </Box>
        <Tabs.Content value="activity" pt="4">
          <ActivityTab address={a.address} initialCategory={initialCategory} />
        </Tabs.Content>
        <Tabs.Content value="tokens" pt="4">
          <TokensTab a={a} />
        </Tabs.Content>
        <Tabs.Content value="staking" pt="4">
          <StakingTab a={a} />
        </Tabs.Content>
      </Tabs.Root>
    </Stack>
  );
}

export function AccountSkeleton() {
  return (
    <Stack gap="6" aria-busy data-testid="account-skeleton">
      <Stack gap="3">
        <HStack gap="3">
          <SkeletonCircle size="12" />
          <Stack gap="2" flex="1">
            <Skeleton h="8" w="48" maxW="full" />
            <Skeleton h="4" w="64" maxW="full" />
          </Stack>
        </HStack>
        <Skeleton h="4" w="96" maxW="full" />
      </Stack>
      <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3">
        {Array.from({ length: 4 }).map((_, i) => (
          <TileSkeleton key={i} />
        ))}
      </SimpleGrid>
      <HStack gap="6" h="44px" borderBottomWidth="1px" borderColor="border.panel" data-testid="tabs-skeleton">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} h="4" w="24" />
        ))}
      </HStack>
    </Stack>
  );
}

/** True for the API's 400 (not a `bze1…` address) as well as a 404. */
function isInvalid(error: unknown): boolean {
  return isNotFound(error) || (error instanceof ApiError && error.status === 400);
}

/** `/accounts/[address]`: the skeleton, then the account (polled), not found for an address the API refuses, or the error state. */
export function AccountPage({
  address,
  initialTab,
  initialCategory,
}: {
  address: string;
  initialTab?: AccountTab;
  initialCategory?: ActivityCategory | null;
}) {
  const { data, error, refetch } = useAccount(address);
  if (data) return <AccountView a={data} initialTab={initialTab} initialCategory={initialCategory} />;
  if (isInvalid(error)) {
    return (
      <Card title="Not a BZE account">
        <Text fontSize="sm" color="fg.muted">
          {accountTitle(address, null)} is not an address the explorer can read. Check that it starts with bze1 and was
          copied whole.
        </Text>
      </Card>
    );
  }
  if (error) {
    return (
      <Card title="This account could not be loaded">
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
  return <AccountSkeleton />;
}
