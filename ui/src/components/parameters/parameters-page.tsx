"use client";

import { Box, Button, Flex, Grid, Heading, HStack, Link, Skeleton, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type MouseEvent } from "react";
import { LuChevronDown, LuTriangleAlert } from "react-icons/lu";

import { useModules, useParams, useStats } from "@/api/hooks";
import type { ChainParams, ModuleAccount } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { CopyButton } from "@/components/ui/copy-button";
import { Chip } from "@/components/ui/chip";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { PageTitle } from "@/components/ui/page-head";
import { TableSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { addressHref } from "@/components/ui/links";
import { shortAddress } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import {
  moduleAccount,
  NOT_AVAILABLE,
  PARAM_GROUPS,
  parameterGroups,
  type GroupDef,
  type ParamGroup,
  type ParamGroupView,
  type ParamRow,
} from "@/lib/parameters";

// Parameter · Value · the info button.
const COLUMNS = "minmax(0, 1fr) minmax(0, 1fr) auto";
const ACCOUNT_COLUMNS = { base: "minmax(0, 1fr) auto", c2: "minmax(0, 46fr) minmax(0, 54fr)" };
const LAYOUT = { base: "minmax(0, 1fr)", c2: "240px minmax(0, 1fr)" };
const LAYOUT_GAP = { base: "4", c2: "6" };
const ADDRESS_JUSTIFY = { base: "flex-end", c2: "flex-start" };
const ADDRESS_ALIGN = { base: "right", c2: "left" } as const;

// The group nav: a sticky side panel from c2 up, wrapping chips below.
const NAV_LOOK = {
  position: { base: "static", c2: "sticky" },
  display: { base: "flex", c2: "block" },
  bg: { base: "transparent", c2: "bg.panel" },
  borderWidth: { base: "0", c2: "1px" },
  py: { base: "0", c2: "6px" },
} as const;
const ITEM_LOOK = {
  px: { base: "10px", c2: "4" },
  py: { base: "6px", c2: "2" },
  fontSize: { base: "13px", c2: "14px" },
  borderWidth: { base: "1px", c2: "0" },
  borderLeftWidth: { base: "1px", c2: "2px" },
  rounded: { base: "l2", c2: "0" },
} as const;
const ITEM_ON = { ...ITEM_LOOK, bg: { base: "bg.panel", c2: "bg.hover" }, borderColor: "accent.fg" } as const;
const ITEM_OFF = { ...ITEM_LOOK, bg: { base: "bg.panel", c2: "transparent" }, borderColor: { base: "border.panel", c2: "transparent" } } as const;

/** The module accounts each group's parameters steer, by the `module` of `/modules`. */
const GROUP_ACCOUNTS: Record<ParamGroup, string[]> = {
  fees: ["fee_collector", "txfeecollector", "txfeecollector_burner", "txfeecollector_cp"],
  staking: ["bonded_tokens_pool", "not_bonded_tokens_pool"],
  inflation: ["mint", "distribution"],
  penalties: [],
  gov: ["gov"],
  dex: ["tradebin"],
  factory: ["tokenfactory"],
  rewards: ["rewards"],
  burner: ["burner", "burner_black_hole", "burner_raffle"],
  cointrunk: [],
};

/** Why a group lists no module account. */
const NO_ACCOUNT: Partial<Record<ParamGroup, string>> = {
  penalties: "Slashing has no module account: penalties are taken straight from the validator's stake.",
  cointrunk: "CoinTrunk has no module account: article fees go straight to the community pool.",
};

const FIRST = PARAM_GROUPS[0].group;

/** A `?group=` value to a group: none or an unknown one is the first (Fees). */
function parseGroup(value: string | null | undefined): ParamGroup {
  return PARAM_GROUPS.find((g) => g.group === value)?.group ?? FIRST;
}

/** A legacy `#dex` anchor to its group, null for anything else. */
function hashGroup(): ParamGroup | null {
  const id = decodeURIComponent(window.location.hash.slice(1));
  return PARAM_GROUPS.find((g) => g.group === id)?.group ?? null;
}

const groupHref = (g: ParamGroup) => `/parameters?group=${g}`;

/** "6 + 2 advanced", or "1 parameter" when none is advanced. */
function countText(rows: ParamRow[]): string {
  const advanced = rows.filter((r) => r.advanced).length;
  if (advanced > 0) return `${rows.length - advanced} + ${advanced} advanced`;
  return `${rows.length} ${rows.length === 1 ? "parameter" : "parameters"}`;
}

/**
 * The groups: a sticky side panel from c2 up (name, module names, count),
 * wrapping chips on phones. A link per group, so a new tab works; a plain
 * click selects it in place.
 */
function GroupNav({ groups, current, onPick }: { groups: (GroupDef & { rows?: ParamRow[] })[]; current: ParamGroup; onPick: (g: ParamGroup) => void }) {
  return (
    <Box
      as="nav"
      aria-label="Parameter groups"
      {...NAV_LOOK}
      top="16px"
      flexWrap="wrap"
      gap="6px"
      borderColor="border.panel"
      rounded="l3"
      overflow="hidden"
      data-testid="param-nav"
    >
      <Text hideBelow="c2" px="4" pt="10px" pb="2" textStyle="label">
        Groups
      </Text>
      {groups.map((g) => {
        const on = g.group === current;
        const n = g.rows?.length;
        return (
          <Link
            key={g.group}
            href={groupHref(g.group)}
            onClick={(e: MouseEvent) => {
              if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
              e.preventDefault();
              onPick(g.group);
            }}
            aria-current={on ? "true" : undefined}
            display="flex"
            alignItems="center"
            justifyContent="space-between"
            gap="10px"
            {...(on ? ITEM_ON : ITEM_OFF)}
            color={on ? "fg" : "fg.muted"}
            _hover={{ color: "fg", bg: "bg.hover", textDecoration: "none" }}
            data-testid="param-nav-item"
          >
            <Flex direction="column" minW="0" fontWeight="500">
              {g.label}
              <Text as="span" hideBelow="c2" fontFamily="mono" fontSize="11px" fontWeight="400" color="fg.muted" truncate>
                {g.modules.join(" · ")}
              </Text>
            </Flex>
            {n != null && (
              <Text as="span" fontFamily="mono" fontSize="12px" color={on ? "accent.fg" : "fg.muted"} minW="22px" textAlign="right">
                {n}
              </Text>
            )}
          </Link>
        );
      })}
    </Box>
  );
}

function GroupHeader({ g, count }: { g: GroupDef; count?: string }) {
  return (
    <Box>
      <Flex align="baseline" gap="10px" wrap="wrap">
        <Heading as="h2" fontSize="20px" fontWeight="600" id={`${g.group}-title`}>
          {g.label}
        </Heading>
        <Text fontFamily="mono" fontSize="12px" color="fg.muted">
          {g.modules.join(" · ")}
        </Text>
        <Box flex="1" />
        {count && (
          <Text fontSize="12px" color="fg.muted" data-testid="param-count">
            {count}
          </Text>
        )}
      </Flex>
      <Text mt="1" mb="10px" fontSize="13px" color="fg.muted">
        {g.description}
        {g.see && (
          <>
            {" "}
            See{" "}
            {g.see.href.startsWith("/") ? (
              <Link asChild color="accent.fg">
                <NextLink href={g.see.href}>{g.see.label}</NextLink>
              </Link>
            ) : (
              <Link href={g.see.href} target="_blank" rel="noopener noreferrer" color="accent.fg">
                {g.see.label}
              </Link>
            )}
            .
          </>
        )}
      </Text>
    </Box>
  );
}

/** The value: module accounts as dashed chips, everything else in mono; "not available" when the chain did not answer. */
function ValueCell({ row }: { row: ParamRow }) {
  if (row.value === null) {
    return (
      <Text fontSize="13px" color="fg.muted" fontStyle="italic">
        {NOT_AVAILABLE}
      </Text>
    );
  }
  const accounts =
    row.def?.format === "address" ? [...new Set(row.values.map(String))].map(moduleAccount).filter((a) => a !== null) : [];
  if (accounts.length > 0) {
    return (
      <HStack gap="2" wrap="wrap">
        {accounts.map((a) => (
          <AccountChip key={a.address} address={a.address} label={a.name ? { name: a.name, kind: "module" } : null} />
        ))}
      </HStack>
    );
  }
  return (
    <Text fontFamily="mono" fontSize="13px" fontWeight="500" color={row.advanced ? "fg.muted" : undefined} wordBreak="break-word" data-testid="param-value">
      {row.value}
    </Text>
  );
}

/** The module a row reads: its dictionary entry's, or the prefix of an advanced row's key. */
const moduleOf = (row: ParamRow) => row.def?.module ?? row.key.split(":")[0];

function Info({ row, id, proposalId }: { row: ParamRow; id: string; proposalId: number | null }) {
  const stored = row.raw || row.value || "";
  return (
    <Box id={id} px="4" pb="3" bg="bg.hover" data-testid="param-info">
      <Box borderLeftWidth="2px" borderColor="accent.fg" ps="14px" py="2px" maxW="760px">
        <Text mb="10px" fontSize="13px" lineHeight="1.55">
          {row.explain}
        </Text>
        <Grid as="dl" templateColumns="max-content minmax(0, 1fr)" columnGap="14px" rowGap="1" fontSize="12px" color="fg.muted">
          <Text as="dt">On chain</Text>
          <Text as="dd" fontFamily="mono" overflowWrap="anywhere">
            {row.chainName}
          </Text>
          {stored && (
            <>
              <Text as="dt">Stored as</Text>
              <Text as="dd" fontFamily="mono" overflowWrap="anywhere" data-testid="param-raw">
                {stored}
              </Text>
            </>
          )}
          {proposalId != null && (
            <>
              <Text as="dt">Last changed by</Text>
              <Text as="dd" fontFamily="mono">
                <Link asChild color="accent.fg">
                  <NextLink href={`/proposals/${proposalId}`} data-testid="last-change-link">
                    #{proposalId}
                  </NextLink>
                </Link>
              </Text>
            </>
          )}
        </Grid>
      </Box>
    </Box>
  );
}

function Row({ row, params }: { row: ParamRow; params: ChainParams }) {
  const [open, setOpen] = useState(false);
  const id = `param-info-${row.key.replace(/[^\w-]/g, "-")}`;
  const proposalId = row.def?.fromStats ? null : (params[moduleOf(row)]?.last_change?.proposal_id ?? null);
  return (
    <Box
      role="listitem"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      data-testid="param-row"
      data-advanced={row.advanced || undefined}
    >
      <ListRow
        templateColumns={COLUMNS}
        cursor="pointer"
        bg={open ? "bg.hover" : undefined}
        borderBottomWidth="0"
        onClick={(e) => {
          // A link inside the row (a module account) goes where it says.
          if ((e.target as HTMLElement).closest("a")) return;
          setOpen((o) => !o);
        }}
      >
        <HStack gap="2" wrap="wrap" minW="0" fontSize="14px" fontWeight={row.advanced ? "500" : "600"} color={row.advanced ? "fg.muted" : "fg"}>
          <Text>{row.name}</Text>
          {row.advanced && <Chip>advanced</Chip>}
        </HStack>
        <Box minW="0">
          <ValueCell row={row} />
        </Box>
        <Button
          size="xs"
          variant="outline"
          h="24px"
          px="2"
          gap="1"
          rounded="l1"
          fontSize="12px"
          fontWeight="400"
          color={open ? "accent.fg" : "fg.muted"}
          borderColor={open ? "accent.fg" : "border.panel"}
          _hover={{ color: "accent.fg", borderColor: "accent.fg", bg: "transparent" }}
          aria-expanded={open}
          aria-controls={open ? id : undefined}
          aria-label={`Info about ${row.name}`}
          onClick={(e) => {
            e.stopPropagation();
            setOpen((o) => !o);
          }}
        >
          info
          <Box as="span" display="inline-flex" transform={open ? "rotate(180deg)" : undefined} transition="transform .15s">
            <LuChevronDown size={12} />
          </Box>
        </Button>
      </ListRow>
      {open && <Info row={row} id={id} proposalId={proposalId} />}
    </Box>
  );
}

function ParamTable({ g, params }: { g: ParamGroupView; params: ChainParams }) {
  return (
    <ListPanel>
      <ListHead templateColumns={COLUMNS} aria-hidden>
        <Text>Parameter</Text>
        <Text>Value</Text>
        <Box w="58px" />
      </ListHead>
      <Box role="list" aria-label={g.label}>
        {g.rows.map((r) => (
          <Row key={r.key} row={r} params={params} />
        ))}
      </Box>
    </ListPanel>
  );
}

function AccountRow({ m }: { m: ModuleAccount }) {
  return (
    <ListRow role="listitem" templateColumns={ACCOUNT_COLUMNS} data-testid="module-account-row">
      <Box minW="0" fontSize="14px">
        <AccountChip address={m.address} label={{ name: m.name, kind: "module" }} />
      </Box>
      <HStack gap="2" minW="0" justify={ADDRESS_JUSTIFY}>
        <Link asChild color="accent.fg" fontFamily="mono" fontSize="13px" minW="0">
          <NextLink href={addressHref(m.address)} title={m.address} data-testid="module-account-address">
            <Text as="span" hideBelow="c3">
              {m.address}
            </Text>
            <Text as="span" hideFrom="c3">
              {shortAddress(m.address)}
            </Text>
          </NextLink>
        </Link>
        <CopyButton value={m.address} label="Copy address" />
      </HStack>
    </ListRow>
  );
}

/** The group's module accounts from `/modules`, by the backend's names, or why it has none. */
function ModuleAccounts({ group }: { group: ParamGroup }) {
  const modules = useModules();
  const note = NO_ACCOUNT[group];
  const wanted = GROUP_ACCOUNTS[group];
  const accounts = wanted.flatMap((name) => modules.data?.filter((m) => m.module === name) ?? []);
  if (!note && !modules.data) return modules.error ? null : <TableSkeleton rows={wanted.length} columns={2} />;
  if (!note && accounts.length === 0) return null;
  return (
    <Box mt="6" data-testid="module-accounts">
      <Heading as="h3" textStyle="label" mb="10px">
        {accounts.length > 1 ? "Module accounts" : "Module account"}
      </Heading>
      {note ? (
        <Text px="4" py="3" fontSize="13px" color="fg.muted" bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3">
          {note}
        </Text>
      ) : (
        <ListPanel>
          <ListHead templateColumns={ACCOUNT_COLUMNS} aria-hidden>
            <Text>Account</Text>
            <Text textAlign={ADDRESS_ALIGN}>Address</Text>
          </ListHead>
          <Box role="list" aria-label={`${accounts.length > 1 ? "Module accounts" : "Module account"} of ${group}`}>
            {accounts.map((m) => (
              <AccountRow key={m.address} m={m} />
            ))}
          </Box>
        </ListPanel>
      )}
    </Box>
  );
}

function CopyAllButton({ params }: { params: ChainParams }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(id);
  }, [copied]);
  return (
    <Button
      size="sm"
      variant="outline"
      borderColor="border.panel"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(JSON.stringify(params, null, 2));
          setCopied(true);
        } catch {
          // No clipboard access (insecure context): nothing to show.
        }
      }}
    >
      {copied ? "Copied" : "Copy all as JSON"}
    </Button>
  );
}

function RawValues({ params }: { params: ChainParams }) {
  return (
    <ListPanel px="18px" py="4" data-testid="raw-values">
      <Flex align="center" justify="space-between" gap="5" wrap="wrap">
        <Box>
          <Text fontWeight="600">Need the raw values?</Text>
          <Text mt="1" fontSize="13px" color="fg.muted" maxW="640px">
            Open <b>info</b> on any row to see what it means, its name on the chain and the value as the chain stores it. Every group is
            also available as JSON.
          </Text>
        </Box>
        <HStack gap="4">
          <CopyAllButton params={params} />
          <Link href={`${externalLinks.pingPub}/params`} target="_blank" rel="noopener noreferrer" color="accent.fg" fontSize="14px">
            Open on Ping.pub ↗
          </Link>
        </HStack>
      </Flex>
    </ListPanel>
  );
}

/** The selected group once loaded: its header, table, module accounts, then the raw values. */
function GroupView({ g, params }: { g: ParamGroupView; params: ChainParams }) {
  return (
    <Stack gap="5" minW="0">
      <Box as="section" id={g.group} aria-labelledby={`${g.group}-title`} data-testid="param-group">
        <GroupHeader g={g} count={countText(g.rows)} />
        <ParamTable g={g} params={params} />
        <ModuleAccounts group={g.group} />
      </Box>
      <RawValues params={params} />
    </Stack>
  );
}

const INTRO =
  "The rules the chain runs by, read live from the chain. Nobody can edit them by hand: a value changes only when a proposal passes, and the proposal that last changed it is linked next to it.";

/**
 * `/parameters`: one group at a time, picked from the side panel and kept in
 * `?group=` (a legacy `#group` anchor selects it too). The values are polled
 * every minute; the explanations also read `/stats`.
 */
export function ParametersPage({ group: initial = null }: { group?: string | null }) {
  const router = useRouter();
  const [group, setGroup] = useState<ParamGroup>(() => parseGroup(initial));
  // A navigation to another `?group=` (the header link, back) moves the selection too.
  const [seen, setSeen] = useState(initial);
  if (seen !== initial) {
    setSeen(initial);
    setGroup(parseGroup(initial));
  }
  const pick = (g: ParamGroup) => {
    setGroup(g);
    router.replace(groupHref(g), { scroll: false });
  };

  // Old links point at `#dex`: select that group, unless `?group=` says otherwise.
  useEffect(() => {
    const fromHash = () => {
      const g = hashGroup();
      if (g) {
        setGroup(g);
        router.replace(groupHref(g), { scroll: false });
      }
    };
    if (!initial) fromHash();
    window.addEventListener("hashchange", fromHash);
    return () => window.removeEventListener("hashchange", fromHash);
  }, [initial, router]);

  const { data, error, refetch } = useParams({ poll: true });
  const stats = useStats();
  // Rows reading `/stats` wait for it, unless it fails: then they say "not available".
  const ready = data && (stats.data || stats.error);
  const groups = ready ? parameterGroups(data, stats.data ?? null) : null;
  const shown = groups?.find((g) => g.group === group);

  return (
    <Box>
      <PageTitle title="Chain parameters" intro={INTRO} />
      <Grid templateColumns={LAYOUT} gap={LAYOUT_GAP} alignItems="start" pb="10">
        <GroupNav groups={groups ?? PARAM_GROUPS} current={group} onPick={pick} />
        {groups && shown ? (
          <GroupView g={shown} params={data!} />
        ) : error ? (
          <StateBox
            tone="error"
            icon={<LuTriangleAlert size={20} />}
            title="The parameters could not be loaded"
            actions={
              <Button size="sm" colorPalette="blue" onClick={() => void refetch()}>
                Try again
              </Button>
            }
          >
            The explorer did not answer. Try again in a moment.
          </StateBox>
        ) : (
          <Stack gap="3" aria-busy data-testid="parameters-skeleton">
            <GroupHeader g={PARAM_GROUPS.find((g) => g.group === group)!} />
            <Skeleton h="4" w="40%" />
            <TableSkeleton rows={4} columns={2} />
          </Stack>
        )}
      </Grid>
    </Box>
  );
}
