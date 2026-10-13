"use client";

import { Box, Button, Flex, Heading, HStack, Link, SimpleGrid, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useId, useMemo, useRef, useState } from "react";
import { LuTriangleAlert } from "react-icons/lu";

import { useAllValidators, useHallOfFame, useParams } from "@/api/hooks";
import type { HallOfFame, HallOfFameRules, Validator, ValidatorList, ValidatorsSummary } from "@/api/types";
import { Avatar } from "@/components/ui/avatar";
import { Chip } from "@/components/ui/chip";
import { FindBox } from "@/components/ui/find-box";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { PageTitle } from "@/components/ui/page-head";
import { TableSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { Tag } from "@/components/ui/status-pill";
import { Tile } from "@/components/ui/tile";
import { VTabPanel, VTabs, VTabsBar } from "@/components/ui/vtabs";
import { formatCount } from "@/lib/format";
import { externalLinks, stakeHref } from "@/lib/links";
import {
  bondCompact,
  filterByMoniker,
  pctText,
  rateText,
  recentVotesText,
  signedBlocksWindow,
  signingText,
  splitValidators,
  unbondingText,
  uptimeText,
  uptimeTone,
} from "@/render/validators";

import { Crown, CrownMark, Moniker } from "./hall-of-fame";

type Fame = Pick<HallOfFame, "items" | "rules">;

/** The four header tiles; each tolerates a null summary field. */
export function SummaryTiles({ summary }: { summary: ValidatorsSummary | null }) {
  const s = summary;
  return (
    <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3" mb="7" data-testid="summary-tiles">
      <Tile
        label="Staked BZE"
        value={s?.bonded_tokens ? bondCompact(s.bonded_tokens) : null}
        sub={s?.staked_share ? `${pctText(s.staked_share)} of all BZE is staked` : null}
      />
      <Tile
        label="Reward rate"
        value={s?.reward_rate ? `≈ ${pctText(s.reward_rate)}` : null}
        sub={
          <Link asChild color="accent.fg">
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
            <Link asChild color="accent.fg">
              <NextLink href="/parameters#staking">see parameters</NextLink>
            </Link>
          </>
        }
      />
      <Tile label="Top 5 validators hold" value={s?.top5_share ? pctText(s.top5_share) : null} sub="a healthier network keeps this low" />
    </SimpleGrid>
  );
}

// # · Validator · Staked (c2) · Commission (c2) · Uptime (c3) · Stake. On phones the stake sits under the name.
// (Custom breakpoints are not in Chakra's generated types, so these objects are hoisted.)
const COLUMNS = {
  base: "24px minmax(0, 1fr) auto",
  c2: "28px minmax(0, 1fr) 170px 100px auto",
  c3: "28px minmax(0, 1fr) 170px 100px 80px auto",
};
const FAME_COLUMNS = {
  base: "24px minmax(0, 1fr) auto",
  c2: "28px minmax(0, 1fr) auto",
};
// The crown's card hangs below its row, so the table panel cannot clip; the last row keeps the rounded corners.
const LAST_ROW = { borderBottomWidth: "0", roundedBottom: "l3" };

function StakeButton({ moniker, operator }: { moniker: string; operator: string }) {
  return (
    <Button asChild size="xs" variant="outline" h="30px" px="3" fontSize="13px" fontWeight="500" borderColor="border.panel">
      <a href={stakeHref(operator)} target="_blank" rel="noopener noreferrer" aria-label={`Stake with ${moniker}`}>
        Stake
      </a>
    </Button>
  );
}

const validatorHref = (operator: string) => `/validators/${encodeURIComponent(operator)}`;

/** "50M BZE · 12.3%": the stake and its share. */
function Staked({ v }: { v: Validator }) {
  return (
    <>
      {bondCompact(v.tokens)}
      {v.voting_power_pct && (
        <Text as="span" color="fg.muted">
          {" "}
          · {pctText(v.voting_power_pct)}
        </Text>
      )}
    </>
  );
}

/** What the lists know of the Hall of Fame: each member's own figures, and the rule. Null while unknown. */
interface FameMarks {
  members: Map<string, Validator>;
  rules: HallOfFameRules;
  seeAll: () => void;
}

function ValidatorRow({ v, fame }: { v: Validator; fame: FameMarks | null }) {
  const member = fame?.members.get(v.operator_address);
  const tone = uptimeTone(v.uptime);
  return (
    <ListRow role="listitem" templateColumns={COLUMNS} _last={LAST_ROW} data-testid="validator-row" data-fame={member ? "" : undefined}>
      <Text fontFamily="mono" color="fg.muted" fontSize="13px">
        {v.status === "bonded" ? (v.rank ?? "—") : "–"}
      </Text>
      <HStack gap="2" minW="0">
        <Avatar name={v.moniker} src={v.logo_url} ring={member ? "gold" : undefined} />
        <Box minW="0">
          <Box wordBreak="break-word">
            <Link asChild fontWeight="500" color="fg" _hover={{ color: "accent.fg" }}>
              <NextLink href={validatorHref(v.operator_address)}>
                <Moniker name={v.moniker} gold={!!member} />
              </NextLink>
            </Link>
            {member && fame && <CrownMark v={member} rules={fame.rules} seeAll={{ onClick: fame.seeAll }} />}
            {v.jailed && (
              <Tag tone="bad" ms="2" verticalAlign="1px">
                Jailed
              </Tag>
            )}
            {v.status === "unbonding" && (
              <Chip ms="2" verticalAlign="1px">
                Unbonding
              </Chip>
            )}
          </Box>
          <Text hideFrom="c2" fontFamily="mono" fontSize="12px" mt="2px">
            <Staked v={v} />
          </Text>
        </Box>
      </HStack>
      <Text hideBelow="c2" textAlign="right" fontFamily="mono" fontSize="13px">
        <Staked v={v} />
      </Text>
      <Text hideBelow="c2" textAlign="right" fontFamily="mono" fontSize="13px">
        {rateText(v.commission_rate)}
      </Text>
      <Text
        hideBelow="c3"
        textAlign="right"
        fontFamily="mono"
        fontSize="13px"
        color={tone === "bad" ? "neg.fg" : tone === "muted" ? "fg.muted" : "fg"}
      >
        {uptimeText(v.uptime)}
      </Text>
      <Box textAlign="right">
        <StakeButton moniker={v.moniker} operator={v.operator_address} />
      </Box>
    </ListRow>
  );
}

function ValidatorTable({ items, empty, fame }: { items: Validator[]; empty: string; fame: FameMarks | null }) {
  return (
    <ListPanel overflow="visible">
      <ListHead templateColumns={COLUMNS} aria-hidden>
        <Text>#</Text>
        <Text>Validator</Text>
        <Text hideBelow="c2" textAlign="right">
          Staked with them
        </Text>
        <Text hideBelow="c2" textAlign="right">
          Commission
        </Text>
        <Text hideBelow="c3" textAlign="right">
          Uptime
        </Text>
        <span />
      </ListHead>
      <Box role="list" aria-label="Validators">
        {items.map((v) => (
          <ValidatorRow key={v.operator_address} v={v} fame={fame} />
        ))}
      </Box>
      {items.length === 0 && (
        <Text px="4" py="6" color="fg.muted" fontSize="sm">
          {empty}
        </Text>
      )}
    </ListPanel>
  );
}

function PickingPanel({ seats }: { seats: number | null }) {
  return (
    <Box as="section" bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" px="5" py="18px" mt="6">
      <Heading as="h2" fontSize="15px" fontWeight="600" mb="2">
        Picking a validator
      </Heading>
      <Text lineHeight="1.55" maxW="760px" mb="2">
        Any active validator pays the same base reward. Lower commission means more for you, and staking with validators outside the top 5
        keeps the network harder to attack.
      </Text>
      <Text color="fg.muted" fontSize="13px" lineHeight="1.55">
        Inactive validators are outside the {seats ? formatCount(seats) : "active"} seats or were jailed, and pay no rewards.
      </Text>
      <Flex gap="5" wrap="wrap" mt="3" fontSize="13px">
        <Link href={externalLinks.staking} target="_blank" rel="noopener noreferrer" color="accent.fg">
          Stake on the Staking app ↗
        </Link>
        <Link href={externalLinks.docs} target="_blank" rel="noopener noreferrer" color="accent.fg">
          How staking works, in the docs ↗
        </Link>
      </Flex>
    </Box>
  );
}

export function ValidatorsSkeleton() {
  return (
    <Stack gap="7" aria-busy data-testid="validators-skeleton">
      <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3">
        {Array.from({ length: 4 }).map((_, i) => (
          <TileSkeleton key={i} />
        ))}
      </SimpleGrid>
      <TableSkeleton rows={10} columns={6} />
    </Stack>
  );
}

function LoadError({ what, onRetry }: { what: string; onRetry: () => void }) {
  return (
    <StateBox
      tone="error"
      icon={<LuTriangleAlert size={20} />}
      title={`${what} could not be loaded`}
      actions={
        <Button size="sm" colorPalette="blue" onClick={onRetry}>
          Try again
        </Button>
      }
    >
      The explorer did not answer. Try again in a moment.
    </StateBox>
  );
}

/** A Hall of Fame row: the validator, its stake and what earned it the place. */
function HallOfFameRow({ v, place, rules, window }: { v: Validator; place: number; rules: HallOfFameRules; window: number }) {
  const within = v.missed_blocks != null && v.missed_blocks <= rules.max_missed_blocks;
  return (
    <ListRow role="listitem" templateColumns={FAME_COLUMNS} _last={LAST_ROW} data-testid="hall-of-fame-row">
      <Text fontFamily="mono" color="fg.muted" fontSize="13px">
        {place}
      </Text>
      <HStack gap="2" minW="0" align="flex-start">
        <Avatar name={v.moniker} src={v.logo_url} ring="gold" />
        <Box minW="0">
          <Link asChild fontWeight="500" wordBreak="break-word">
            <NextLink href={validatorHref(v.operator_address)}>
              <Moniker name={v.moniker} gold />
            </NextLink>
          </Link>
          <Text fontFamily="mono" fontSize="12px" color="fg.muted" mt="2px">
            {bondCompact(v.tokens)}
            {v.voting_power_pct ? ` · ${pctText(v.voting_power_pct)} of the voting power` : ""}
          </Text>
          <Flex gap="6px" wrap="wrap" mt="6px">
            <Chip variant={within ? "ok" : "warn"} whiteSpace="normal" data-testid="hall-of-fame-signing">
              {signingText(v, rules.signed_blocks_window ?? window)}
            </Chip>
            <Chip variant="acc" whiteSpace="normal" data-testid="hall-of-fame-votes">
              {recentVotesText(v)}
            </Chip>
          </Flex>
        </Box>
      </HStack>
      <StakeButton moniker={v.moniker} operator={v.operator_address} />
    </ListRow>
  );
}

/** The Hall of Fame tab: the rule in words, the members in the API's order (the smallest first). */
function HallOfFameList({ fame, query, window }: { fame: Fame; query: string; window: number }) {
  const { items, rules } = fame;
  const rows = filterByMoniker(items, query);
  // While fewer proposals have closed than the rule counts, the members voted on all of those.
  const proposals = Math.min(rules.proposals, items[0]?.recent_votes.of ?? rules.proposals);
  return (
    <>
      <Text color="fg.muted" fontSize="13px" maxW="720px" mb="14px" lineHeight="1.55" data-testid="hall-of-fame-intro">
        Active validators that voted on every one of the last {formatCount(proposals)} proposals, whatever their vote, and missed no more
        than {formatCount(rules.max_missed_blocks)}
        {rules.signed_blocks_window != null ? ` of the last ${formatCount(rules.signed_blocks_window)}` : ""} blocks. The smallest come
        first: staking with them spreads the voting power.
      </Text>
      <ListPanel>
        {rows.length === 0 ? (
          <Text px="4" py="6" color="fg.muted" fontSize="sm">
            {query ? "No Hall of Fame validator has that name." : "No validator meets the bar right now."}
          </Text>
        ) : (
          <Box role="list" aria-label="Hall of Fame">
            {rows.map((v) => (
              <HallOfFameRow key={v.operator_address} v={v} place={items.indexOf(v) + 1} rules={rules} window={window} />
            ))}
          </Box>
        )}
      </ListPanel>
    </>
  );
}

type Tab = "fame" | "active" | "inactive";

interface FameQuery {
  data: Fame | null;
  failed: boolean;
  retry: () => void;
}

/**
 * The list once loaded: tiles, the tabs with the filter, the table and the
 * footnotes. The Hall of Fame loads on its own: until it answers, nobody is
 * marked and its tab waits, and the rest of the page works.
 */
export function ValidatorsView({ list, fame }: { list: ValidatorList; fame: FameQuery }) {
  const [tab, setTab] = useState<Tab>("fame");
  const [query, setQuery] = useState("");
  const id = useId();
  const bar = useRef<HTMLDivElement>(null);
  const params = useParams();
  const { active, inactive } = useMemo(() => splitValidators(list.items), [list.items]);
  const seats = list.summary?.max_validators ?? null;
  const window = signedBlocksWindow(params.data) ?? fame.data?.rules.signed_blocks_window ?? 10_000;

  const showFame = () => {
    setTab("fame");
    bar.current?.scrollIntoView?.({ block: "start" });
  };
  const fameItems = fame.data?.items;
  const members = useMemo(() => new Map((fameItems ?? []).map((v) => [v.operator_address, v])), [fameItems]);
  const marks: FameMarks | null = fame.data ? { members, rules: fame.data.rules, seeAll: showFame } : null;

  const footnote = (
    <Text color="fg.muted" fontSize="13px">
      Commission is the share of your rewards the validator keeps. Uptime is the share of the last {formatCount(window)} blocks the
      validator signed.
    </Text>
  );

  return (
    <Box>
      <SummaryTiles summary={list.summary} />
      <Box ref={bar} scrollMarginTop="4">
        <VTabsBar>
          <VTabs
            id={id}
            label="Validators"
            value={tab}
            onChange={setTab}
            tabs={[
              {
                value: "fame",
                icon: <Crown />,
                label: "Hall of Fame",
                count: fame.data ? formatCount(fame.data.items.length) : null,
              },
              {
                value: "active",
                label: "Active",
                count: formatCount(active.length),
              },
              {
                value: "inactive",
                label: "Inactive",
                count: formatCount(inactive.length),
              },
            ]}
          />
          <FindBox label="Find a validator" value={query} onChange={setQuery} mb="2" />
        </VTabsBar>
      </Box>
      <VTabPanel id={id} value={tab}>
        {tab === "fame" ? (
          fame.data ? (
            <HallOfFameList fame={fame.data} query={query} window={window} />
          ) : fame.failed ? (
            <LoadError what="The Hall of Fame" onRetry={fame.retry} />
          ) : (
            <TableSkeleton rows={5} columns={3} />
          )
        ) : (
          <ValidatorTable
            items={filterByMoniker(tab === "active" ? active : inactive, query)}
            empty={
              tab === "active"
                ? query
                  ? "No active validator has that name."
                  : "No active validators."
                : query
                  ? "No inactive validator has that name."
                  : "Every validator is active."
            }
            fame={marks}
          />
        )}
      </VTabPanel>
      <Flex direction="column" align="flex-start" gap="14px" mt="3">
        {tab === "fame" && (
          <Button
            variant="outline"
            h="36px"
            px="14px"
            fontSize="13px"
            fontWeight="500"
            borderColor="border.panel"
            onClick={() => setTab("active")}
          >
            See all active validators
          </Button>
        )}
        {footnote}
      </Flex>
      <PickingPanel seats={seats} />
    </Box>
  );
}

/** `/validators`: the skeleton on the first load, then the list (polled; never a skeleton again). */
export function ValidatorsPage() {
  const list = useAllValidators();
  const fame = useHallOfFame();
  const data = list.data;
  const activeCount = data ? splitValidators(data.items).active.length : 0;
  const seats = data?.summary?.max_validators ?? null;
  return (
    <Box>
      <PageTitle
        title="Validators"
        intro={
          data ? (
            <>
              {formatCount(activeCount)} validators produce blocks right now
              {seats ? `, out of ${formatCount(seats)} seats` : ""}. Validators run the chain and earn rewards for the people who stake BZE
              with them. They do not hold your BZE: staked coins stay in your account.
            </>
          ) : undefined
        }
      />
      {data ? (
        <ValidatorsView
          list={data}
          fame={{
            data: fame.data ?? null,
            failed: !!fame.error && !fame.data,
            retry: () => void fame.refetch(),
          }}
        />
      ) : list.error ? (
        <LoadError what="The validators" onRetry={() => void list.refetch()} />
      ) : (
        <ValidatorsSkeleton />
      )}
    </Box>
  );
}
