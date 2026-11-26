"use client";

import { Box, Button, Flex, Link, SimpleGrid, Skeleton, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useId, useMemo, useState, type ReactNode } from "react";
import { LuCircleHelp, LuTriangleAlert } from "react-icons/lu";

import { isNotFound } from "@/api/client";
import { useDao, useDaos, useDenoms } from "@/api/hooks";
import type { Account, Balance, Dao, DaoSummary } from "@/api/types";
import { TokensTab } from "@/components/accounts/tokens-tab";
import { Amount } from "@/components/ui/amount";
import { Card } from "@/components/ui/card";
import { Chip } from "@/components/ui/chip";
import { CopyButton } from "@/components/ui/copy-button";
import { DetailList, DetailRow } from "@/components/ui/detail-list";
import { ExternalLink } from "@/components/ui/external-link";
import { JsonBlock } from "@/components/ui/json-block";
import { addressHref } from "@/components/ui/links";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { MoreDetails } from "@/components/ui/more-details";
import { Breadcrumb, DetailMeta, DetailTitle } from "@/components/ui/page-head";
import { TileSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { Tile } from "@/components/ui/tile";
import { VTabPanel, VTabs, VTabsBar } from "@/components/ui/vtabs";
import { formatCount, formatDateTime, formatUsd } from "@/lib/format";
import { daoAppLinks } from "@/lib/links";
import { isAboveZero } from "@/lib/math";
import { tokenRows, tokensTotalUsd } from "@/render/account";
import { destinationText, durationText, parseDaoTab, ratioText, refundText, sourceKind, type DaoTab } from "@/render/daos";
import { tokenHref } from "@/render/tokens";

import { DaoHistoryTab } from "./dao-history-tab";
import { DaoProposalsTab } from "./dao-proposals-tab";
import { TypeTag, WhoChip } from "./parts";

const TILE_COLUMNS = { base: 1, sm: 2, lg: 4 };

/** Keeps the tab in the URL without a navigation, so a link opens the same tab; the first tab leaves it out. */
function writeTab(tab: string, isDefault: boolean) {
  const url = new URL(window.location.href);
  url.search = isDefault ? "" : `?tab=${tab}`;
  window.history.replaceState(window.history.state, "", url);
}

/** The addresses of the DAOs the explorer lists, to name a DAO that runs another. */
function useDaoBook(): Map<string, DaoSummary> {
  const list = useDaos(null);
  return useMemo(() => {
    const book = new Map<string, DaoSummary>();
    for (const page of list.data?.pages ?? []) for (const d of page.items) book.set(d.address, d);
    return book;
  }, [list.data]);
}

/** "Self-governed", "Run by …" and the handover when one is pending. */
function StatusLine({ d, daos }: { d: Dao; daos: Map<string, DaoSummary> }) {
  const pending = d.live.pending_admin;
  return (
    <Flex gap="2" align="center" wrap="wrap" data-testid="dao-status">
      {d.self_governed ? (
        <Text as="span" data-testid="dao-admin-state">
          Self-governed
        </Text>
      ) : (
        <>
          <Text as="span">Run by</Text>
          <WhoChip address={d.admin} label={d.admin_label} daos={daos} />
        </>
      )}
      {pending && (
        <>
          <Text as="span">·</Text>
          <Chip variant="warn" data-testid="dao-handover">
            Admin handover pending to
          </Chip>
          <WhoChip address={pending} daos={daos} />
        </>
      )}
    </Flex>
  );
}

/** "Run by DAO #7 · Name, which is run by …": the admin chain, up to the account that ends it. */
function AdminChain({ d, daos }: { d: Dao; daos: Map<string, DaoSummary> }) {
  const c = d.controller;
  if (!c || d.self_governed) return null;
  const chain: string[] = [];
  for (const address of [d.admin, ...c.path.slice(1), c.address]) {
    if (chain[chain.length - 1] !== address) chain.push(address);
  }
  const first = d.admin_label?.kind === "dao" || daos.has(d.admin);
  if (chain.length < 2 && !first) return null;
  const note =
    c.end_reason === "cycle"
      ? "The chain loops back on itself."
      : c.end_reason === "depth_cap"
        ? "The chain is longer than the explorer follows."
        : null;
  return (
    <Flex gap="1" align="center" wrap="wrap" mt="2" fontSize="13px" color="fg.muted" data-testid="dao-admin-chain">
      {chain.map((address, i) => (
        <Flex key={address} gap="1" align="center" wrap="wrap">
          <Text as="span">{i === 0 ? "Run by" : ", which is run by"}</Text>
          <WhoChip address={address} label={i === 0 ? d.admin_label : address === c.address ? c.label : undefined} daos={daos} />
        </Flex>
      ))}
      {note && <Text as="span">· {note}</Text>}
    </Flex>
  );
}

function treasuryAccount(d: Dao): Account {
  return {
    address: d.address,
    label: d.address_label,
    first_seen: null,
    last_seen_height: null,
    tx_count: 0,
    activity_count: 0,
    balances: d.treasury,
    delegations: [],
    unbonding: [],
    rewards: [],
    total_staked: "0",
    total_rewards: [],
    live: { available: d.live.available },
  };
}

function coinText(c: Balance | null | undefined) {
  return c && isAboveZero(c.amount) ? <Amount amount={c.amount} denom={c.denom} symbol={c.symbol} exponent={c.exponent} compact /> : null;
}

function Tiles({ d }: { d: Dao }) {
  const denoms = useDenoms();
  const live = d.live.available;
  const cfg = d.live.config;
  const rows = tokenRows(d.treasury, denoms.data, "value");
  const stake = d.live.electorate_stake;
  const cost = coinText(cfg?.proposal_cost);
  const deposit = coinText(cfg?.proposal_deposit);
  const free = (
    <Text as="span" color="fg.muted">
      free
    </Text>
  );
  return (
    <SimpleGrid columns={TILE_COLUMNS} gap="3" mt="6" data-testid="dao-tiles">
      <Tile
        label="Treasury"
        value={live ? formatUsd(tokensTotalUsd(rows)) : null}
        sub={live ? `${formatCount(rows.length)} ${rows.length === 1 ? "token" : "tokens"}, at DEX prices` : null}
      />
      {d.type === "stake" ? (
        <Tile
          label="Voting stake"
          value={stake ? <Amount amount={stake.voting_stake.amount} denom={stake.voting_stake.denom} symbol={stake.voting_stake.symbol} exponent={stake.voting_stake.exponent} compact /> : null}
          sub={stake ? `in ${formatCount(stake.sources.length)} ${stake.sources.length === 1 ? "program" : "programs"}` : null}
        />
      ) : (
        <Tile
          label="Members"
          value={live ? formatCount(d.live.members?.length ?? 0) : null}
          sub={live ? "one vote each" : null}
        />
      )}
      <Tile
        label="Proposal cost"
        value={live ? (cost ?? free) : null}
        sub={live ? (deposit ? <>deposit {deposit}</> : "no deposit") : null}
      />
      <Tile
        label="Voting period"
        value={cfg ? durationText(cfg.voting_period_seconds) : null}
        sub={cfg ? (cfg.allow_expedite ? `expedited: ${durationText(cfg.expedited_period_seconds)}` : "no expedited voting") : null}
        small
      />
    </SimpleGrid>
  );
}

function Unavailable({ children }: { children: string }) {
  return (
    <Text fontSize="13px" color="fg.muted" data-testid="tab-unavailable">
      {children}
    </Text>
  );
}

function TreasuryTab({ d }: { d: Dao }) {
  return (
    <Box>
      <Flex justify="flex-end" mb="2" fontSize="13px">
        <Link asChild color="accent.fg">
          <NextLink href={addressHref(d.address)} data-testid="treasury-account-link">
            Open the account page →
          </NextLink>
        </Link>
      </Flex>
      <TokensTab a={treasuryAccount(d)} module />
    </Box>
  );
}

const MEMBER_COLUMNS = { base: "minmax(0, 1fr)" };

function MembersTab({ d }: { d: Dao }) {
  const members = d.live.members ?? [];
  if (!d.live.available) return <Unavailable>The members will show once the node answers again.</Unavailable>;
  return (
    <Box>
      <Text fontSize="13px" color="fg.muted" mb="14px">
        {formatCount(members.length)} {members.length === 1 ? "member" : "members"}, one vote each.
      </Text>
      <ListPanel>
        <Box role="list" aria-label="Members">
          {members.map((address) => (
            <ListRow key={address} role="listitem" templateColumns={MEMBER_COLUMNS} fontSize="13px" data-testid="dao-member">
              <Flex gap="2" align="center" minW="0">
                <WhoChip address={address} />
                <CopyButton value={address} label="Copy address" />
              </Flex>
            </ListRow>
          ))}
        </Box>
        {members.length === 0 && (
          <Text px="4" py="6" color="fg.muted" fontSize="13px">
            This DAO has no members.
          </Text>
        )}
      </ListPanel>
    </Box>
  );
}

const SOURCE_COLUMNS = { base: "minmax(0, 1fr) auto", c2: "minmax(0, 1.4fr) minmax(0, 1fr) minmax(0, 1fr)" };

function ElectorateTab({ d }: { d: Dao }) {
  if (!d.live.available) return <Unavailable>The voters will show once the node answers again.</Unavailable>;
  const sources = d.live.electorate_stake?.sources;
  const electorate = d.live.electorate;
  return (
    <Box>
      <Text fontSize="13px" color="fg.muted" mb="14px" maxW="720px" data-testid="electorate-explainer">
        Voting power = stake in these programs, counted only if it joined before the proposal.
      </Text>
      <ListPanel>
        <ListHead templateColumns={SOURCE_COLUMNS} aria-hidden>
          <Text>Program</Text>
          <Text hideBelow="c2">Staking token</Text>
          <Text textAlign="right">Staked now</Text>
        </ListHead>
        <Box role="list" aria-label="Electorate">
          {sources
            ? sources.map((s) => (
                <ListRow key={s.sr_id ?? `dr-${s.dr_denom}`} role="listitem" templateColumns={SOURCE_COLUMNS} fontSize="13px" data-testid="dao-source">
                  <Box minW="0">
                    <Text overflowWrap="anywhere">{sourceKind(s)}</Text>
                    {/* The staking token's own column starts at c2; on phones its link sits under the program. */}
                    <Link asChild hideFrom="c2" fontSize="12px" color="accent.fg">
                      <NextLink href={tokenHref(s.staking_denom)} data-testid="source-token-phone">
                        {s.staked_amount.symbol ?? s.staking_denom}
                      </NextLink>
                    </Link>
                  </Box>
                  <Box hideBelow="c2" minW="0">
                    <Link asChild color="accent.fg" overflowWrap="anywhere">
                      <NextLink href={tokenHref(s.staking_denom)}>{s.staked_amount.symbol ?? s.staking_denom}</NextLink>
                    </Link>
                  </Box>
                  <Text textAlign="right" fontFamily="mono">
                    <Amount amount={s.staked_amount.amount} denom={s.staked_amount.denom} symbol={s.staked_amount.symbol} exponent={s.staked_amount.exponent} />
                  </Text>
                </ListRow>
              ))
            : (electorate?.sr_ids ?? []).map((id) => (
                <ListRow key={id} role="listitem" templateColumns={SOURCE_COLUMNS} fontSize="13px" data-testid="dao-source">
                  <Text>{`Staking reward ${id}`}</Text>
                  <Box hideBelow="c2" />
                  <Text textAlign="right" color="fg.muted">
                    —
                  </Text>
                </ListRow>
              ))}
        </Box>
        {!sources && !electorate?.sr_ids?.length && (
          <Text px="4" py="6" color="fg.muted" fontSize="13px">
            This DAO counts no stake yet.
          </Text>
        )}
      </ListPanel>
    </Box>
  );
}

function Raw({ value, children }: { value: string; children: ReactNode }) {
  return (
    <Text as="span" title={value} data-raw={value}>
      {children}
    </Text>
  );
}

function RulesTab({ d }: { d: Dao }) {
  const c = d.live.config;
  if (!c) return <Unavailable>The rules will show once the node answers again.</Unavailable>;
  const cost = coinText(c.proposal_cost);
  const deposit = coinText(c.proposal_deposit);
  return (
    <Card p="0" data-testid="dao-rules">
      <DetailList>
        <DetailRow label="Quorum">
          <Raw value={c.min_quorum}>{ratioText(c.min_quorum)} of the voting power must vote</Raw>
        </DetailRow>
        <DetailRow label="Threshold">
          <Raw value={c.threshold}>More than {ratioText(c.threshold)} of the yes and no votes must be yes</Raw>
        </DetailRow>
        <DetailRow label="Voting period">
          <Raw value={`${c.voting_period_seconds}s`}>{durationText(c.voting_period_seconds)}</Raw>
        </DetailRow>
        <DetailRow label="Expedited voting">
          {c.allow_expedite ? (
            <Raw value={`${c.expedited_period_seconds}s, quorum ${c.expedite_quorum}, threshold ${c.expedite_threshold}`}>
              Allowed: {durationText(c.expedited_period_seconds)}, needs {ratioText(c.expedite_quorum)} of the voting power to vote and more than {ratioText(c.expedite_threshold)} of it to say yes
            </Raw>
          ) : (
            <Text as="span" color="fg.muted">
              Not allowed
            </Text>
          )}
        </DetailRow>
        <DetailRow label="Who can propose">
          <Raw value={String(c.open_submission)}>{c.open_submission ? "Anyone" : "Only voters of the DAO"}</Raw>
        </DetailRow>
        <DetailRow label="Proposal cost">
          {cost ? (
            <>
              {cost} <Text as="span" color="fg.muted" title={c.cost_destination}>· {destinationText(c.cost_destination)}</Text>
            </>
          ) : (
            <Text as="span" color="fg.muted">
              Free
            </Text>
          )}
        </DetailRow>
        <DetailRow label="Proposal deposit" last>
          {deposit ? (
            <>
              {deposit}{" "}
              <Text as="span" color="fg.muted" title={`${c.deposit_refund_policy} / ${c.kept_deposit_destination}`}>
                · {refundText(c.deposit_refund_policy)}
                {c.deposit_refund_policy.toUpperCase().endsWith("ALWAYS") ? "" : `; a kept deposit is ${destinationText(c.kept_deposit_destination)}`}
              </Text>
            </>
          ) : (
            <Text as="span" color="fg.muted">
              None
            </Text>
          )}
        </DetailRow>
      </DetailList>
      {d.live.description && (
        <Box px="4" py="3" borderTopWidth="1px" borderColor="border.panel" fontSize="13px" color="fg.muted" data-testid="dao-description">
          {d.live.description}
        </Box>
      )}
    </Card>
  );
}

function View({ d, initialTab }: { d: Dao; initialTab?: string }) {
  const id = useId();
  const daos = useDaoBook();
  const [tab, setTab] = useState<DaoTab>(parseDaoTab(initialTab));
  const stake = d.type === "stake";
  const members = d.live.members?.length;
  return (
    <Box>
      <Breadcrumb path={[{ label: "DAOs", href: "/daos" }]} current={`DAO #${d.id}`} />
      <Flex gap="4" align="flex-start" justify="space-between" wrap="wrap" mt="3">
        <Box flex="1" minW="0" flexBasis="260px">
          <DetailTitle title={d.name || `DAO #${d.id}`}>
            <Chip data-testid="dao-id">DAO #{d.id}</Chip>
            <TypeTag type={d.type} />
          </DetailTitle>
          <DetailMeta data-testid="dao-meta">
            <StatusLine d={d} daos={daos} />
          </DetailMeta>
          <AdminChain d={d} daos={daos} />
          <DetailMeta mt="2" gap="1" align="flex-start">
            <Text as="span" fontFamily="mono" color="fg" wordBreak="break-all" data-testid="dao-address">
              {d.address}
            </Text>
            <CopyButton value={d.address} label="Copy address" />
          </DetailMeta>
          {d.live.created_at && (
            <DetailMeta mt="1">Created {formatDateTime(d.live.created_at, { seconds: false })}</DetailMeta>
          )}
        </Box>
        <ExternalLink href={daoAppLinks.dao(d.id)} fontSize="13px" fontWeight="500" data-testid="dao-app-link">
          Open in the DAO app
        </ExternalLink>
      </Flex>
      <Tiles d={d} />
      {!d.live.available && (
        <Text mt="3" fontSize="13px" color="fg.muted" data-testid="live-unavailable">
          Live data is unavailable right now: the node did not answer. The history below is still here.
        </Text>
      )}
      <VTabsBar mt="8">
        <VTabs
          id={id}
          label="DAO"
          value={tab}
          onChange={(t) => {
            setTab(t);
            writeTab(t, t === "treasury");
          }}
          tabs={[
            { value: "treasury", label: "Treasury", count: d.live.available ? formatCount(d.treasury.length) : null },
            stake
              ? { value: "members", label: "Electorate", count: d.live.electorate_stake ? formatCount(d.live.electorate_stake.sources.length) : null }
              : { value: "members", label: "Members", count: members != null ? formatCount(members) : null },
            { value: "proposals", label: "Proposals" },
            { value: "rules", label: "Rules" },
            { value: "history", label: "History" },
          ]}
        />
      </VTabsBar>
      <VTabPanel id={id} value={tab}>
        {tab === "treasury" && <TreasuryTab d={d} />}
        {tab === "members" && (stake ? <ElectorateTab d={d} /> : <MembersTab d={d} />)}
        {tab === "proposals" && <DaoProposalsTab dao={d} />}
        {tab === "rules" && <RulesTab d={d} />}
        {tab === "history" && <DaoHistoryTab dao={d} daos={daos} />}
      </VTabPanel>
      <Box mt="8">
        <MoreDetails hint="raw JSON of the DAO as the chain holds it">
          <JsonBlock value={d.live} />
        </MoreDetails>
      </Box>
    </Box>
  );
}

function Skeletons() {
  return (
    <Stack gap="6" pt="6" aria-busy data-testid="dao-skeleton">
      <Skeleton h="3" w="32" />
      <Skeleton h="8" w="48" maxW="full" />
      <Skeleton h="4" w="96" maxW="full" />
      <SimpleGrid columns={TILE_COLUMNS} gap="3">
        {Array.from({ length: 4 }).map((_, i) => (
          <TileSkeleton key={i} />
        ))}
      </SimpleGrid>
    </Stack>
  );
}

function MissingHead({ id }: { id: number }) {
  return (
    <Box>
      <Breadcrumb path={[{ label: "DAOs", href: "/daos" }]} current={`DAO #${id}`} />
    </Box>
  );
}

/** `/daos/[id]`: the skeleton, then the DAO (polled), not found on a 404, or the error state. */
export function DaoPage({ id, initialTab }: { id: number; initialTab?: string }) {
  const { data, error, refetch } = useDao(id);
  if (data) return <View d={data} initialTab={initialTab} />;
  if (isNotFound(error)) {
    return (
      <Box>
        <MissingHead id={id} />
        <Box mt="6">
          <StateBox icon={<LuCircleHelp size={20} />} title="DAO not found" data-testid="dao-not-found">
            The explorer does not know DAO #{id}. Check the link, or find it in the{" "}
            <Link asChild color="accent.fg">
              <NextLink href="/daos">DAOs list</NextLink>
            </Link>
            .
          </StateBox>
        </Box>
      </Box>
    );
  }
  if (error) {
    return (
      <Box>
        <MissingHead id={id} />
        <Box mt="6">
          <StateBox
            tone="error"
            icon={<LuTriangleAlert size={20} />}
            title="This DAO could not be loaded"
            data-testid="dao-error"
            actions={
              <Button size="sm" colorPalette="blue" onClick={() => void refetch()}>
                Try again
              </Button>
            }
          >
            The explorer did not answer. Try again in a moment.
          </StateBox>
        </Box>
      </Box>
    );
  }
  return <Skeletons />;
}
