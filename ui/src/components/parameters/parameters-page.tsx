"use client";

import { Badge, Box, Button, Grid, Heading, HStack, Link, Skeleton, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useEffect, useState } from "react";
import { LuCheck, LuCopy, LuExternalLink } from "react-icons/lu";

import { useParams, useProposal, useStats } from "@/api/hooks";
import type { ChainParams, Stats } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Card } from "@/components/ui/card";
import { TableSkeleton } from "@/components/ui/skeleton";
import { formatCount, formatDate } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import {
  latestChange,
  moduleAccount,
  NOT_AVAILABLE,
  PARAM_GROUPS,
  parameterGroups,
  type ParamGroupView,
  type ParamRow,
} from "@/lib/parameters";

const COLUMNS = { base: "minmax(0, 1fr)", md: "minmax(0, 1.1fr) minmax(0, 1fr) minmax(0, 2fr)" };

function AnchorNav() {
  return (
    <HStack as="nav" aria-label="Parameter groups" gap="2" wrap="wrap" data-testid="param-nav">
      {PARAM_GROUPS.map((g) => (
        <Button key={g.group} asChild size="sm" variant="outline" rounded="full" minH="44px">
          <a href={`#${g.group}`}>{g.label}</a>
        </Button>
      ))}
    </HStack>
  );
}

function ProposalLink({ id }: { id: number }) {
  const { data } = useProposal(id);
  return (
    <Link asChild>
      <NextLink href={`/proposals/${id}`} data-testid="last-change-link">
        #{id}
        {data ? ` ${data.title}` : ""}
      </NextLink>
    </Link>
  );
}

/** "Last change #44 V8 Upgrade · Nov 24, 2025 · DEX, Rewards programs". */
function LastChange({ params }: { params: ChainParams }) {
  const change = latestChange(params);
  if (!change) return null;
  return (
    <Text fontSize="sm" color="fg.muted" data-testid="last-change">
      Last change{" "}
      {change.proposalId != null ? (
        <ProposalLink id={change.proposalId} />
      ) : (
        <>
          in block{" "}
          <Link asChild>
            <NextLink href={`/blocks/${change.height}`}>{formatCount(change.height)}</NextLink>
          </Link>
        </>
      )}{" "}
      · {formatDate(change.time)}
      {change.groups.length > 0 && ` · ${change.groups.join(", ")}`}
    </Text>
  );
}

/** The value cell: module accounts as chips, everything else as text; the stored value in grey under it. */
function ValueCell({ row }: { row: ParamRow }) {
  const accounts =
    row.value !== null && row.def?.format === "address"
      ? [...new Set(row.values.map(String))].map(moduleAccount).filter((a) => a !== null)
      : [];
  return (
    <Stack gap="0.5" minW="0">
      {row.value === null ? (
        <Text fontSize="sm" color="fg.muted" fontStyle="italic">
          {NOT_AVAILABLE}
        </Text>
      ) : accounts.length > 0 ? (
        <HStack gap="2" wrap="wrap">
          {accounts.map((a) => (
            <AccountChip key={a.address} address={a.address} label={a.name ? { name: a.name, kind: "module" } : null} />
          ))}
        </HStack>
      ) : (
        <Text fontSize="sm" fontWeight="medium" wordBreak="break-word" data-testid="param-value">
          {row.value}
        </Text>
      )}
      {row.raw && (
        <Text fontSize="xs" fontFamily="mono" color="fg.subtle" wordBreak="break-all" data-testid="param-raw">
          {row.raw}
        </Text>
      )}
    </Stack>
  );
}

function Row({ row }: { row: ParamRow }) {
  return (
    <Grid
      role="row"
      templateColumns={COLUMNS}
      gap={{ base: "1.5", md: "4" }}
      px="4"
      py="3"
      borderTopWidth="1px"
      borderColor="border.panel"
      data-testid="param-row"
      data-advanced={row.advanced || undefined}
    >
      <Stack gap="0.5" minW="0" role="cell">
        <HStack gap="2" wrap="wrap">
          <Text fontSize="sm" fontWeight="semibold">
            {row.name}
          </Text>
          {row.advanced && (
            <Badge size="sm" variant="subtle">
              advanced
            </Badge>
          )}
        </HStack>
        <Text fontSize="xs" fontFamily="mono" color="fg.subtle" wordBreak="break-all">
          {row.chainName}
        </Text>
      </Stack>
      <Box role="cell" minW="0">
        <ValueCell row={row} />
      </Box>
      <Text role="cell" fontSize="sm" color="fg.muted">
        {row.explain}
      </Text>
    </Grid>
  );
}

function TableHead() {
  return (
    <Grid
      role="row"
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
    >
      <Text role="columnheader">Parameter</Text>
      <Text role="columnheader">Value</Text>
      <Text role="columnheader">What it means</Text>
    </Grid>
  );
}

function GroupHeader({ g }: { g: Omit<ParamGroupView, "rows" | "changedBy"> & { changedBy?: ParamGroupView["changedBy"] } }) {
  return (
    <Stack gap="1">
      <HStack gap="3" wrap="wrap" align="baseline">
        <Heading as="h2" size="lg" id={`${g.group}-title`}>
          {g.label}
        </Heading>
        <Text fontSize="sm" fontFamily="mono" color="fg.subtle">
          {g.modules.join(" · ")}
        </Text>
        {g.changedBy && (
          <Link asChild fontSize="sm">
            <NextLink href={`/proposals/${g.changedBy.proposalId}`} data-testid="changed-by">
              changed by #{g.changedBy.proposalId}
            </NextLink>
          </Link>
        )}
      </HStack>
      <Text fontSize="sm" color="fg.muted">
        {g.description}
        {g.see && (
          <>
            {" "}
            See{" "}
            {g.see.href.startsWith("/") ? (
              <Link asChild>
                <NextLink href={g.see.href}>{g.see.label}</NextLink>
              </Link>
            ) : (
              <Link href={g.see.href} target="_blank" rel="noopener noreferrer">
                {g.see.label}
              </Link>
            )}
            .
          </>
        )}
      </Text>
    </Stack>
  );
}

function Group({ g }: { g: ParamGroupView }) {
  return (
    <Stack as="section" id={g.group} gap="3" scrollMarginTop="24" aria-labelledby={`${g.group}-title`} data-testid="param-group">
      <GroupHeader g={g} />
      <Box role="table" aria-label={g.label} bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" overflow="hidden">
        <TableHead />
        {g.rows.map((r) => (
          <Row key={r.key} row={r} />
        ))}
      </Box>
    </Stack>
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
      variant="outline"
      minH="44px"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(JSON.stringify(params, null, 2));
          setCopied(true);
        } catch {
          // No clipboard access (insecure context): nothing to show.
        }
      }}
    >
      {copied ? <LuCheck aria-hidden /> : <LuCopy aria-hidden />}
      {copied ? "Copied" : "Copy all as JSON"}
    </Button>
  );
}

function RawValues({ params }: { params: ChainParams }) {
  return (
    <Card title="Need the raw values?" data-testid="raw-values">
      <Stack gap="4">
        <Text fontSize="sm" color="fg.muted">
          The small grey name under each parameter is its name on the chain, and the grey value is what the chain stores.
          Every group is also available as JSON.
        </Text>
        <HStack gap="3" wrap="wrap">
          <CopyAllButton params={params} />
          <Button asChild variant="ghost" minH="44px">
            <a href={`${externalLinks.pingPub}/params`} target="_blank" rel="noopener noreferrer">
              Open on Ping.pub <LuExternalLink aria-hidden />
            </a>
          </Button>
        </HStack>
      </Stack>
    </Card>
  );
}

const INTRO =
  "The rules the chain runs by, read live from the chain. Nobody can edit them by hand: a value changes only when a proposal passes, and the proposal that last changed it is linked next to it.";

/**
 * The page once loaded: every group from the live values. A `#group` in the
 * URL is scrolled to again once the groups render, because the browser
 * scrolled to the skeleton's (shorter) section before the data arrived.
 */
export function ParametersView({ params, stats }: { params: ChainParams; stats: Stats | null }) {
  const groups = parameterGroups(params, stats);
  useEffect(() => {
    const id = decodeURIComponent(window.location.hash.slice(1));
    if (id) document.getElementById(id)?.scrollIntoView();
  }, []);
  return (
    <Stack gap={{ base: "8", md: "10" }}>
      <Stack gap="3">
        <LastChange params={params} />
        <AnchorNav />
      </Stack>
      {groups.map((g) => (
        <Group key={g.group} g={g} />
      ))}
      <RawValues params={params} />
    </Stack>
  );
}

/** The first load: the anchor nav, then each group's header and three skeleton rows. */
export function ParametersSkeleton() {
  return (
    <Stack gap={{ base: "8", md: "10" }} aria-busy data-testid="parameters-skeleton">
      <Stack gap="3">
        <Skeleton h="4" w="60%" />
        <AnchorNav />
      </Stack>
      {PARAM_GROUPS.map((g) => (
        <Stack key={g.group} as="section" id={g.group} gap="3" scrollMarginTop="24">
          <GroupHeader g={g} />
          <TableSkeleton rows={3} columns={3} />
        </Stack>
      ))}
    </Stack>
  );
}

/** `/parameters`: the live values, polled every minute (the explanations also read `/stats`). */
export function ParametersPage() {
  const { data, error, refetch } = useParams({ poll: true });
  const stats = useStats();
  // Rows reading `/stats` wait for it, unless it fails: then they say "not available".
  const ready = data && (stats.data || stats.error);
  return (
    <Stack gap="6">
      <Stack gap="2">
        <Heading as="h1" size={{ base: "2xl", md: "3xl" }}>
          Chain parameters
        </Heading>
        <Text color="fg.muted" maxW="3xl">
          {INTRO}
        </Text>
      </Stack>
      {ready ? (
        <ParametersView params={data} stats={stats.data ?? null} />
      ) : error ? (
        <Card title="The parameters could not be loaded">
          <HStack justify="space-between" wrap="wrap" gap="3">
            <Text color="fg.muted" fontSize="sm">
              The explorer did not answer. Try again in a moment.
            </Text>
            <Button colorPalette="blue" minH="44px" onClick={() => void refetch()}>
              Try again
            </Button>
          </HStack>
        </Card>
      ) : (
        <ParametersSkeleton />
      )}
    </Stack>
  );
}
