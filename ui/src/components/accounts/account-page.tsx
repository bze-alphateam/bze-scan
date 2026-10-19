"use client";

import { Box, Button, Flex, HStack, Link, NativeSelect, SimpleGrid, Skeleton, SkeletonCircle, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useRouter } from "next/navigation";
import { Fragment, useId, useState, type ReactNode } from "react";
import { LuTriangleAlert } from "react-icons/lu";

import { AppLink } from "@/components/layout/app-link";
import { ApiError, isNotFound } from "@/api/client";
import { useAccount, useAllValidators, useDenoms, useModules } from "@/api/hooks";
import type { Account, ActivityCategory, ModuleAccount } from "@/api/types";
import { Avatar } from "@/components/ui/avatar";
import { Callout } from "@/components/ui/callout";
import { Chip } from "@/components/ui/chip";
import { CopyButton } from "@/components/ui/copy-button";
import { addressHref } from "@/components/ui/links";
import { Breadcrumb, DetailMeta, DetailTitle } from "@/components/ui/page-head";
import { TileSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { Tile } from "@/components/ui/tile";
import { VTabPanel, VTabs, VTabsBar } from "@/components/ui/vtabs";
import { formatCount, formatUsd, shortAddress } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { usdValue } from "@/lib/math";
import {
  activeSince,
  amountOf,
  BOND_DENOM,
  daysText,
  moduleBondWhere,
  moduleFacts,
  parseModuleTab,
  parseTab,
  tokenRows,
  tokensTotalUsd,
  unstaking,
  type AccountTab,
  type ModuleFacts,
  type ModuleTab,
} from "@/render/account";
import { BOND, bondCompact } from "@/render/validators";

import { ActivityTab, EmptyPanel } from "./activity-tab";
import { StakingTab } from "./staking-tab";
import { TokensTab } from "./tokens-tab";

const TILE_COLUMNS = { base: 1, sm: 2, lg: 4 };

/** Keeps the tab in the URL without a navigation, so a link opens the same tab; the default tab leaves it out. */
function writeTab(tab: string, isDefault: boolean) {
  const url = new URL(window.location.href);
  url.search = isDefault ? "" : `?tab=${tab}`;
  window.history.replaceState(window.history.state, "", url);
}

/** A sentence whose `words` in backticks are module names, shown in mono. */
function WithCode({ text }: { text: string }) {
  return (
    <>
      {text.split("`").map((part, i) =>
        i % 2 === 1 ? (
          <Text as="span" key={i} fontFamily="mono">
            {part}
          </Text>
        ) : (
          <Fragment key={i}>{part}</Fragment>
        ),
      )}
    </>
  );
}

/** The header both kinds share: breadcrumb, 56px avatar, h1 with chips, meta lines, the address with copy, and a right slot. */
function Head({
  a,
  crumb,
  avatar,
  title,
  chips,
  meta,
  side,
}: {
  a: Account;
  crumb: string;
  avatar: ReactNode;
  title: string;
  chips: ReactNode;
  meta: ReactNode;
  side?: ReactNode;
}) {
  return (
    <Box>
      <Breadcrumb path={[{ label: "Home", href: "/" }]} current={crumb} />
      <Flex gap="4" align="flex-start" wrap="wrap" mt="3">
        {avatar}
        <Box flex="1" minW="0" flexBasis="240px">
          <DetailTitle title={title}>{chips}</DetailTitle>
          <DetailMeta data-testid="account-since">{meta}</DetailMeta>
          <DetailMeta mt="1" gap="1" align="flex-start">
            <Text as="span" fontFamily="mono" color="fg" wordBreak="break-all" data-testid="account-address">
              {a.address}
            </Text>
            <CopyButton value={a.address} label="Copy address" />
          </DetailMeta>
        </Box>
        {side}
      </Flex>
    </Box>
  );
}

function PersonalHead({ a }: { a: Account }) {
  const validators = useAllValidators();
  // A validator owner's label is the validator's moniker.
  const owned =
    a.label?.kind === "validator_owner" ? validators.data?.items.find((v) => v.moniker === a.label?.name) : undefined;
  const escrow = a.label?.kind === "ibc_escrow";
  return (
    <Head
      a={a}
      crumb="Account"
      avatar={<Avatar name={a.address.slice(4)} src={owned?.logo_url} size="56px" />}
      title={a.label?.name ?? "Account"}
      chips={
        <>
          <Chip data-testid="kind-chip">{escrow ? "Cross-chain escrow" : "Personal account"}</Chip>
          {a.label?.kind === "validator_owner" && (
            <Chip variant="acc" data-testid="label-chip">
              {owned ? (
                <Link asChild color="inherit">
                  <NextLink href={`/validators/${encodeURIComponent(owned.operator_address)}`}>owner of {a.label.name}</NextLink>
                </Link>
              ) : (
                `owner of ${a.label.name}`
              )}
            </Chip>
          )}
        </>
      }
      meta={activeSince(a)}
    />
  );
}

function LiveUnavailable({ children }: { children: string }) {
  return (
    <Text mt="3" fontSize="13px" color="fg.muted" data-testid="live-unavailable">
      {children}
    </Text>
  );
}

function PersonalTiles({ a }: { a: Account }) {
  const denoms = useDenoms();
  if (!a.live.available) {
    return (
      <Box mt="6">
        <SimpleGrid columns={TILE_COLUMNS} gap="3" data-testid="account-tiles">
          <Tile label="Available" value={null} />
          <Tile label="Staked" value={null} />
          <Tile label="Rewards to claim" value={null} />
          <Tile label="Unstaking" value={null} />
        </SimpleGrid>
        <LiveUnavailable>
          Live balances are unavailable right now: the node did not answer. Transactions and activity are still here.
        </LiveUnavailable>
      </Box>
    );
  }
  const available = amountOf(a.balances, BOND_DENOM);
  const usd = usdValue(available, BOND.exponent, denoms.data?.get(BOND_DENOM)?.price_usd);
  const unbonding = unstaking(a.unbonding, new Date());
  const n = a.delegations.length;
  return (
    <SimpleGrid columns={TILE_COLUMNS} gap="3" mt="6" data-testid="account-tiles">
      <Tile label="Available" value={bondCompact(available)} sub={usd != null ? formatUsd(usd) : null} />
      <Tile
        label="Staked"
        value={bondCompact(a.total_staked ?? "0")}
        sub={n === 0 ? "not staking" : `with ${formatCount(n)} ${n === 1 ? "validator" : "validators"}`}
      />
      <Tile
        label="Rewards to claim"
        value={bondCompact(amountOf(a.total_rewards, BOND_DENOM))}
        sub={
          <>
            Claim on{" "}
            <AppLink app="staking" href={externalLinks.staking}>
              Staking
            </AppLink>
          </>
        }
      />
      <Tile
        label="Unstaking"
        value={
          unbonding.nextDays == null ? (
            <Text as="span" color="fg.muted">
              none
            </Text>
          ) : (
            bondCompact(unbonding.total)
          )
        }
        sub={unbonding.nextDays == null ? null : `next unlock in ${daysText(unbonding.nextDays)}`}
      />
    </SimpleGrid>
  );
}

/** A personal account: header, tiles, and the Activity / Tokens / Staking tabs. */
function PersonalView({
  a,
  initialTab,
  initialCategory,
  initialPage,
}: {
  a: Account;
  initialTab?: string;
  initialCategory: ActivityCategory | null;
  initialPage: number;
}) {
  const id = useId();
  const [tab, setTab] = useState<AccountTab>(parseTab(initialTab));
  const live = a.live.available;
  const tokens = live ? tokenRows(a.balances, undefined, "name").length : null;
  const n = a.delegations.length;
  return (
    <Box>
      <PersonalHead a={a} />
      <PersonalTiles a={a} />
      <VTabsBar mt="8">
        <VTabs
          id={id}
          label="Account"
          value={tab}
          onChange={(t) => {
            setTab(t);
            writeTab(t, t === "activity");
          }}
          tabs={[
            { value: "activity", label: "Activity", count: formatCount(a.activity_count) },
            { value: "tokens", label: "Tokens", count: tokens != null ? formatCount(tokens) : null },
            {
              value: "staking",
              label: "Staking",
              count: live ? `${formatCount(n)} ${n === 1 ? "validator" : "validators"}` : null,
            },
          ]}
        />
      </VTabsBar>
      <VTabPanel id={id} value={tab}>
        {tab === "activity" && (
          <ActivityTab
            address={a.address}
            initialCategory={initialCategory}
            initialPage={initialPage}
            neverSeen={a.activity_count === 0}
          />
        )}
        {tab === "tokens" && <TokensTab a={a} />}
        {tab === "staking" && <StakingTab a={a} />}
      </VTabPanel>
    </Box>
  );
}

/** On a module account: a menu of the chain's other module accounts, to move between them. */
function ModuleSwitcher({ address, modules }: { address: string; modules: ModuleAccount[] | undefined }) {
  const router = useRouter();
  if (!modules || modules.length < 2) return null;
  return (
    <Stack gap="1" fontSize="12px" w={{ base: "full", sm: "auto" }}>
      <Text asChild color="fg.muted">
        <label htmlFor="module-switcher">Other module accounts</label>
      </Text>
      <NativeSelect.Root size="sm" minW="220px">
        <NativeSelect.Field
          id="module-switcher"
          data-testid="module-switcher"
          value={address}
          onChange={(e) => router.push(addressHref(e.currentTarget.value))}
        >
          {modules.map((m) => (
            <option key={m.address} value={m.address}>
              {m.name}
            </option>
          ))}
        </NativeSelect.Field>
        <NativeSelect.Indicator />
      </NativeSelect.Root>
    </Stack>
  );
}

function ModuleTiles({ a, facts, moduleName }: { a: Account; facts: ModuleFacts | null; moduleName: string | null }) {
  const denoms = useDenoms();
  const live = a.live.available;
  const rows = live ? tokenRows(a.balances, denoms.data, "value") : [];
  return (
    <Box mt="6">
      <SimpleGrid columns={TILE_COLUMNS} gap="3" data-testid="account-tiles">
        <Tile
          label="Holds"
          value={live ? formatUsd(tokensTotalUsd(rows)) : null}
          sub={live ? `${formatCount(rows.length)} ${rows.length === 1 ? "token" : "tokens"}, at DEX prices` : null}
        />
        <Tile label="BZE held" value={live ? bondCompact(amountOf(a.balances, BOND_DENOM)) : null} sub={moduleBondWhere(moduleName)} />
        <Tile label="Moves when" value={facts?.moves ?? null} sub={facts?.movesSub} />
        <Tile
          label="Who can sign"
          value={
            <Text as="span" color="fg.muted">
              nobody
            </Text>
          }
          sub="no private key exists"
        />
      </SimpleGrid>
      {!live && <LiveUnavailable>Live balances are unavailable right now: the node did not answer.</LiveUnavailable>}
    </Box>
  );
}

/** A module account: the chain owns it, so no activity or staking, its holdings and what moves its coins instead. */
function ModuleView({ a, initialTab }: { a: Account; initialTab?: string }) {
  const id = useId();
  const [tab, setTab] = useState<ModuleTab>(parseModuleTab(initialTab));
  const modules = useModules();
  const name = a.label?.name ?? "Module";
  const moduleName = modules.data?.find((m) => m.address === a.address)?.module ?? null;
  // The texts wait for the module list; a module it lacks gets the plain fallback.
  const facts = modules.data || modules.error ? moduleFacts(moduleName) : null;
  const holdings = a.live.available ? tokenRows(a.balances, undefined, "name").length : null;
  return (
    <Box>
      <Head
        a={a}
        crumb="Module account"
        avatar={<Avatar name={name} variant="module" size="56px" borderWidth="2px" />}
        title={name}
        chips={
          <Chip variant="modc" data-testid="kind-chip">
            Module account
          </Chip>
        }
        meta={facts ? <Text as="span" data-testid="module-owner"><WithCode text={facts.owner} /></Text> : null}
        side={<ModuleSwitcher address={a.address} modules={modules.data} />}
      />
      <ModuleTiles a={a} facts={facts} moduleName={moduleName} />
      <Callout tone="acc" mt="5" title="This account belongs to the chain, not to a person" data-testid="module-callout">
        Nobody holds a key to it, so nobody can send from it. Its coins move only by the {name}&apos;s own rules, as a side
        effect of other people&apos;s transactions. It never stakes and earns no staking rewards.
      </Callout>
      <VTabsBar mt="8">
        <VTabs
          id={id}
          label="Module account"
          value={tab}
          onChange={(t) => {
            setTab(t);
            writeTab(t, t === "tokens");
          }}
          tabs={[
            { value: "tokens", label: "Holdings", count: holdings != null ? formatCount(holdings) : null },
            { value: "movements", label: "Movements" },
          ]}
        />
      </VTabsBar>
      <VTabPanel id={id} value={tab}>
        {tab === "tokens" ? (
          <TokensTab a={a} module />
        ) : (
          <EmptyPanel title="Module accounts don't send transactions" testId="module-movements">
            Coins move in and out of the {name} account inside other people&apos;s transactions. Each of those shows it as
            sender or receiver under &quot;Where the funds went&quot;.{" "}
            <Link asChild color="accent.fg">
              <NextLink href="/txs">See the latest transactions →</NextLink>
            </Link>
          </EmptyPanel>
        )}
      </VTabPanel>
    </Box>
  );
}

interface AccountViewProps {
  a: Account;
  /** The raw `?tab=`: each kind reads its own tabs from it. */
  initialTab?: string;
  initialCategory?: ActivityCategory | null;
  initialPage?: number;
}

/** A loaded account: the module design for an account the chain owns (label kind `module`), else the personal one. */
export function AccountView({ a, initialTab, initialCategory = null, initialPage = 1 }: AccountViewProps) {
  if (a.label?.kind === "module") return <ModuleView a={a} initialTab={initialTab} />;
  return <PersonalView a={a} initialTab={initialTab} initialCategory={initialCategory} initialPage={initialPage} />;
}

export function AccountSkeleton() {
  return (
    <Stack gap="6" pt="6" aria-busy data-testid="account-skeleton">
      <Skeleton h="3" w="32" />
      <HStack gap="4" align="start">
        <SkeletonCircle size="14" />
        <Stack gap="2" flex="1">
          <Skeleton h="8" w="48" maxW="full" />
          <Skeleton h="4" w="64" maxW="full" />
          <Skeleton h="4" w="96" maxW="full" />
        </Stack>
      </HStack>
      <SimpleGrid columns={TILE_COLUMNS} gap="3">
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

/** The breadcrumb, "Account" and the address over a state box. */
function Missing({ address, children }: { address: string; children: ReactNode }) {
  return (
    <Box>
      <Breadcrumb path={[{ label: "Home", href: "/" }]} current="Account" />
      <DetailTitle title="Account" />
      <DetailMeta>
        <Text as="span" fontFamily="mono" wordBreak="break-all">
          {address}
        </Text>
      </DetailMeta>
      <Box mt="6">{children}</Box>
    </Box>
  );
}

/** `/accounts/[address]`: the skeleton, then the account (polled), not a BZE account for an address the API refuses, or the error state. */
export function AccountPage({
  address,
  initialTab,
  initialCategory,
  initialPage,
}: {
  address: string;
  initialTab?: string;
  initialCategory?: ActivityCategory | null;
  initialPage?: number;
}) {
  const { data, error, refetch } = useAccount(address);
  if (data) return <AccountView a={data} initialTab={initialTab} initialCategory={initialCategory} initialPage={initialPage} />;
  if (isInvalid(error)) {
    return (
      <Missing address={address}>
        <StateBox tone="error" icon={<LuTriangleAlert size={20} />} title="Not a BZE account" data-testid="account-invalid">
          <Text as="span" fontFamily="mono">
            {shortAddress(address)}
          </Text>{" "}
          is not an address the explorer can read. Check that it starts with bze1 and was copied whole.
        </StateBox>
      </Missing>
    );
  }
  if (error) {
    return (
      <Missing address={address}>
        <StateBox
          tone="error"
          icon={<LuTriangleAlert size={20} />}
          title="This account could not be loaded"
          data-testid="account-error"
          actions={
            <Button size="sm" colorPalette="blue" onClick={() => void refetch()}>
              Try again
            </Button>
          }
        >
          The explorer did not answer. Try again in a moment.
        </StateBox>
      </Missing>
    );
  }
  return <AccountSkeleton />;
}
