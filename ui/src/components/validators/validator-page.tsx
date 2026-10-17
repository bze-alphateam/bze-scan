"use client";

import {
  Box,
  Button,
  Flex,
  Grid,
  Heading,
  HStack,
  Link,
  SimpleGrid,
  Skeleton,
  SkeletonCircle,
  Stack,
  Text,
  type LinkProps,
} from "@chakra-ui/react";
import NextLink from "next/link";
import type { ReactNode } from "react";
import { LuCircleHelp, LuExternalLink, LuTriangleAlert } from "react-icons/lu";

import { isNotFound } from "@/api/client";
import { useAllValidators, useHallOfFame, useParams, useValidator } from "@/api/hooks";
import type { HallOfFame, ValidatorDetail, ValidatorsSummary, ValidatorVote } from "@/api/types";
import { AppIcon } from "@/components/layout/app-icon";
import { AccountChip } from "@/components/ui/account-chip";
import { Avatar } from "@/components/ui/avatar";
import { CopyButton } from "@/components/ui/copy-button";
import { DetailList, DetailRow } from "@/components/ui/detail-list";
import { JsonBlock } from "@/components/ui/json-block";
import { ListPanel, ListRow } from "@/components/ui/list";
import { MoreDetails } from "@/components/ui/more-details";
import { Breadcrumb as Breadcrumbs, DetailMeta, DetailTitle, PanelHead } from "@/components/ui/page-head";
import { CardSkeleton, TileSkeleton } from "@/components/ui/skeleton";
import { StateBox } from "@/components/ui/state-box";
import { Tag, type TagTone } from "@/components/ui/status-pill";
import { Tile } from "@/components/ui/tile";
import { TimeAgo } from "@/components/ui/time-ago";
import { formatAmount, formatCount, formatDate, shortAddress } from "@/lib/format";
import { externalLinks, stakeHref, websiteLink } from "@/lib/links";
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
  voteTone,
} from "@/render/validators";

import { CrownMark, Moniker } from "./hall-of-fame";

type Fame = Pick<HallOfFame, "items" | "rules">;

export function pingPubValidatorUrl(operator: string): string {
  return `${externalLinks.pingPub}/staking/${operator}`;
}

const Muted = ({ children }: { children: ReactNode }) => (
  <Text as="span" color="fg.muted">
    {children}
  </Text>
);

const Mono = ({ children }: { children: ReactNode }) => (
  <Text as="span" fontFamily="mono">
    {children}
  </Text>
);

/** "Validators / Honeycomb". */
function Breadcrumb({ current, mono }: { current: string; mono?: boolean }) {
  return <Breadcrumbs path={[{ label: "Validators", href: "/validators" }]} current={current} mono={mono} />;
}

function statusTone(v: ValidatorDetail): TagTone {
  if (v.jailed || v.tombstoned) return "bad";
  return v.status === "bonded" ? "ok" : "warn";
}

function Header({ v, activeCount, fame }: { v: ValidatorDetail; activeCount: number | null; fame: Fame | null }) {
  const site = websiteLink(v.website);
  const member = fame?.items.find((m) => m.operator_address === v.operator_address);
  return (
    <Box>
      <Breadcrumb current={v.moniker} />
      <Flex gap="4" align="flex-start" wrap="wrap" mt="3">
        <Avatar name={v.moniker} src={v.logo_url} size="56px" ring={member ? "gold" : undefined} />
        <Box flex="1" minW="240px">
          <DetailTitle title={<Moniker name={v.moniker} gold={!!member} />}>
            {member && fame && <CrownMark v={member} rules={fame.rules} seeAll={{ href: "/validators" }} size={18} />}
          </DetailTitle>
          <DetailMeta>
            <Tag tone={statusTone(v)} data-testid="validator-status">
              {statusLine(v, activeCount)}
            </Tag>
            {site && (
              <Link href={site.href} target="_blank" rel="noopener noreferrer nofollow" color="accent.fg">
                {site.text} ↗
              </Link>
            )}
          </DetailMeta>
          <DetailMeta mt="1">
            <Text as="span">
              {[
                v.first_seen_time ? `Validating since ${formatDate(v.first_seen_time)}` : null,
                v.delegator_count != null
                  ? `${formatCount(v.delegator_count)} ${v.delegator_count === 1 ? "account stakes" : "accounts stake"} here`
                  : null,
              ]
                .filter(Boolean)
                .join(" · ")}
            </Text>
          </DetailMeta>
        </Box>
        <Button asChild colorPalette="blue" h="40px" px="4" fontWeight="600">
          <a href={stakeHref(v.operator_address)} target="_blank" rel="noopener noreferrer">
            <AppIcon app="staking" size={18} />
            Stake with this validator <LuExternalLink aria-hidden />
          </a>
        </Button>
      </Flex>
    </Box>
  );
}

function Tiles({ v, summary, window }: { v: ValidatorDetail; summary: ValidatorsSummary | null; window: number | null }) {
  const yours = rewardRateAfterCommission(summary?.reward_rate, v.commission_rate);
  const signedWindow = v.signed_blocks_window ?? window;
  return (
    <SimpleGrid columns={{ base: 1, sm: 2, lg: 4 }} gap="3" mt="6" data-testid="validator-tiles">
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

function About({ v }: { v: ValidatorDetail }) {
  const rows: [string, ReactNode][] = [
    [
      "Validator address",
      <>
        <Text as="span" fontFamily="mono" fontSize="12px" wordBreak="break-all">
          {v.operator_address}
        </Text>
        <CopyButton value={v.operator_address} label="Copy validator address" />
      </>,
    ],
    [
      "Owner account",
      <>
        <AccountChip address={v.account_address} />
        <CopyButton value={v.account_address} label="Copy owner account" /> <Muted>· self-stake and commission land here</Muted>
      </>,
    ],
    [
      "Self-stake",
      <>
        <Mono>{v.self_delegation != null ? bondFull(v.self_delegation) : "—"}</Mono> <Muted>· the validator&apos;s own money at risk</Muted>
      </>,
    ],
  ];
  if (v.security_contact) rows.push(["Contact", <Text key="contact" as="span">{v.security_contact}</Text>]);
  if (v.identity) rows.push(["Identity", <Mono key="identity">{v.identity}</Mono>]);
  return (
    <ListPanel as="section" aria-labelledby="validator-about" mt="7">
      <PanelHead id="validator-about" title="About" />
      {v.details && (
        <Text px="4" py="14px" lineHeight="1.55" whiteSpace="pre-line" borderBottomWidth="1px" borderColor="border.panel">
          {v.details}
        </Text>
      )}
      <DetailList>
        {rows.map(([label, value], i) => (
          <DetailRow key={label} label={label} last={i === rows.length - 1}>
            {value}
          </DetailRow>
        ))}
      </DetailList>
    </ListPanel>
  );
}

function RecentBlocks({ v }: { v: ValidatorDetail }) {
  const blocks = v.recent_blocks;
  return (
    <ListPanel as="section" aria-labelledby="validator-blocks">
      <PanelHead id="validator-blocks" title="Recent blocks proposed" note={proposalFrequency(v.voting_power_pct)} />
      {blocks.length === 0 ? (
        <Text px="4" py="3" fontSize="13px" color="fg.muted">
          No blocks proposed yet.
        </Text>
      ) : (
        <Box role="list" aria-label="Recent blocks proposed">
          {blocks.map((b) => (
            <ListRow key={b.height} role="listitem" templateColumns="auto minmax(0, 1fr) auto" data-testid="recent-block">
              <Link asChild fontFamily="mono" fontWeight="500" color="accent.fg">
                <NextLink href={`/blocks/${b.height}`} aria-label={`Block ${formatCount(b.height)}`}>
                  {formatCount(b.height)}
                </NextLink>
              </Link>
              <TimeAgo time={b.time} color="fg.muted" fontSize="13px" />
              <Text color="fg.muted" fontSize="13px" textAlign="right">
                {b.tx_count === 0 ? "no transactions" : `${formatCount(b.tx_count)} ${b.tx_count === 1 ? "transaction" : "transactions"}`}
              </Text>
            </ListRow>
          ))}
        </Box>
      )}
    </ListPanel>
  );
}

/** A vote's tag: Yes in green, No in red, veto in the warn tone, split in the accent, abstain muted. */
function VoteTag({ vote }: { vote: ValidatorVote }) {
  const tone = voteTone(vote);
  const props =
    tone === "gray"
      ? { bg: "bg.hover", color: "fg.muted" }
      : {
          tone: (
            {
              green: "ok",
              red: "bad",
              orange: "warn",
              purple: "accent",
            } as const
          )[tone],
        };
  return (
    <Tag flex="none" minW="30px" justifyContent="center" {...props}>
      {voteOption(vote)}
    </Tag>
  );
}

function History({ v }: { v: ValidatorDetail }) {
  const items = historyItems(v.events);
  const clean = cleanRecord(v);
  return (
    <ListPanel as="section" aria-labelledby="validator-history">
      <PanelHead id="validator-history" title="History" note="changes worth knowing about" />
      {clean && (
        <Flex align="center" gap="10px" wrap="wrap" px="4" py="3" borderBottomWidth="1px" borderColor="border.panel">
          <Tag tone="ok">Clean</Tag>
          <Text as="span">{clean}</Text>
        </Flex>
      )}
      {items.length > 0 && (
        <Box role="list" aria-label="Validator history">
          {items.map((h) => (
            <Box key={h.key} role="listitem" px="4" py="10px" borderBottomWidth="1px" borderColor="border.panel" data-testid="history-item">
              <Text>{h.text}</Text>
              <HStack gap="1" color="fg.muted" fontSize="12px" mt="2px">
                <Text as="span">{h.date}</Text>
                {h.txHash && (
                  <>
                    <Text as="span">·</Text>
                    <Link asChild color="accent.fg">
                      <NextLink href={`/txs/${h.txHash.toUpperCase()}`}>see transaction</NextLink>
                    </Link>
                  </>
                )}
              </HStack>
            </Box>
          ))}
        </Box>
      )}
      <Box data-testid="validator-votes">
        {v.votes.length === 0 ? (
          <Text px="4" py="3" color="fg.muted" fontSize="13px">
            {v.moniker} has not voted yet.
          </Text>
        ) : (
          <>
            <Text px="4" pt="3" pb="6px" fontSize="11px" fontWeight="600" letterSpacing="0.06em" textTransform="uppercase" color="fg.muted">
              Last governance votes
            </Text>
            <Box role="list" aria-label="Last governance votes">
              {v.votes.map((vote) => (
                <Flex
                  key={`${vote.proposal_id}-${vote.height}`}
                  role="listitem"
                  align="baseline"
                  gap="10px"
                  px="4"
                  py="7px"
                  borderTopWidth="1px"
                  borderColor="border.panel"
                  data-testid="validator-vote"
                >
                  <VoteTag vote={vote} />
                  <Link asChild color="fg" minW="0" _hover={{ color: "accent.fg" }}>
                    <NextLink href={`/proposals/${vote.proposal_id}`}>
                      <Text as="span" fontFamily="mono" color="fg.muted">
                        #{vote.proposal_id}
                      </Text>
                      {vote.title ? ` ${vote.title}` : ""}
                    </NextLink>
                  </Link>
                </Flex>
              ))}
            </Box>
          </>
        )}
      </Box>
    </ListPanel>
  );
}

/** "Open on Ping.pub ↗": on the raw bar from c2 up, under the JSON on phones. */
function PingPubOpen({ operator, ...props }: { operator: string } & Omit<LinkProps, "href" | "children">) {
  return (
    <Link
      href={pingPubValidatorUrl(operator)}
      target="_blank"
      rel="noopener noreferrer"
      fontSize="12px"
      color="accent.fg"
      whiteSpace="nowrap"
      {...props}
    >
      Open on Ping.pub ↗
    </Link>
  );
}

function Details({ v }: { v: ValidatorDetail }) {
  return (
    <ListPanel>
      <DetailList>
        <DetailRow label="Consensus key">
          <Text as="span" fontFamily="mono" fontSize="12px" wordBreak="break-all">
            {v.consensus_pubkey ?? "—"}
          </Text>
          {v.consensus_pubkey && <CopyButton value={v.consensus_pubkey} label="Copy consensus key" />}
        </DetailRow>
        <DetailRow label="Commission limits">
          <Muted>max</Muted> <Mono>{rateText(v.commission_max_rate)}</Mono> <Muted>· max change</Muted>{" "}
          <Mono>{pointsText(v.commission_max_change_rate)}</Mono> <Muted>per day</Muted>
        </DetailRow>
        <DetailRow label="Minimum self-stake">
          <Mono>{v.min_self_delegation != null ? formatAmount(v.min_self_delegation, 0, "ubze") : "—"}</Mono>
        </DetailRow>
        <DetailRow label="Delegator shares" last>
          <Mono>{formatAmount(v.delegator_shares, 6)}</Mono>
        </DetailRow>
      </DetailList>
      <HStack gap="0.5" px="2" borderTopWidth="1px" borderColor="border.panel">
        <Heading
          as="h3"
          px="2.5"
          py="10px"
          fontSize="13px"
          fontWeight="500"
          borderBottomWidth="2px"
          borderColor="accent.fg"
          whiteSpace="nowrap"
        >
          Raw validator
        </Heading>
        <Box flex="1" />
        <PingPubOpen operator={v.operator_address} hideBelow="c2" px="2" />
      </HStack>
      <Box p="4" pt="3" borderTopWidth="1px" borderColor="border.panel">
        <JsonBlock value={v} />
      </Box>
      <PingPubOpen operator={v.operator_address} hideFrom="c2" display="inline-block" px="4" pb="4" />
    </ListPanel>
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
  // Membership marks wait for the Hall of Fame; the page never does.
  const fame = useHallOfFame();
  const activeCount = all.data ? splitValidators(all.data.items).active.length : null;
  return (
    <Box>
      <Header v={v} activeCount={activeCount} fame={fame.data ?? null} />
      <Tiles v={v} summary={all.data?.summary ?? null} window={signedBlocksWindow(params.data)} />
      <About v={v} />
      <Grid
        templateColumns={{
          base: "minmax(0, 1fr)",
          lg: "repeat(2, minmax(0, 1fr))",
        }}
        gap="4"
        alignItems="start"
        mt="8"
      >
        <RecentBlocks v={v} />
        <History v={v} />
      </Grid>
      <Box mt="8">
        <MoreDetails hint="consensus key, limits, raw JSON">
          <Details v={v} />
        </MoreDetails>
      </Box>
    </Box>
  );
}

/** The breadcrumb and "Validator" over a state box: not found, or the load error. */
function Missing({ operator, children }: { operator: string; children: ReactNode }) {
  return (
    <Box>
      <Breadcrumb current={shortAddress(operator)} mono />
      <DetailTitle title="Validator" />
      <Box mt="6">{children}</Box>
    </Box>
  );
}

/** `/validators/[operator]`: the skeleton, then the validator (polled), not found on a 404, or the error state. */
export function ValidatorPage({ operator }: { operator: string }) {
  const { data, error, refetch } = useValidator(operator);
  if (data) return <ValidatorView v={data} />;
  if (isNotFound(error)) {
    return (
      <Missing operator={operator}>
        <StateBox
          icon={<LuCircleHelp size={20} />}
          title="Validator not found"
          actions={
            <Link href={pingPubValidatorUrl(operator)} target="_blank" rel="noopener noreferrer" color="accent.fg">
              Look it up on Ping.pub ↗
            </Link>
          }
          data-testid="validator-not_found"
        >
          The explorer does not know this validator yet. Check the address, or look it up on Ping.pub.
        </StateBox>
      </Missing>
    );
  }
  if (error) {
    return (
      <Missing operator={operator}>
        <StateBox
          tone="error"
          icon={<LuTriangleAlert size={20} />}
          title="This validator could not be loaded"
          actions={
            <Button size="sm" colorPalette="blue" onClick={() => void refetch()}>
              Try again
            </Button>
          }
          data-testid="validator-error"
        >
          The explorer did not answer. Try again in a moment.
        </StateBox>
      </Missing>
    );
  }
  return <ValidatorSkeleton />;
}
