"use client";

import { Box, Grid, HStack, Link, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";

import { useModules } from "@/api/hooks";
import type { Proposal, TokenSummary } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { Chip } from "@/components/ui/chip";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount, formatDate } from "@/lib/format";
import { externalLinks } from "@/lib/links";
import { describeParam, groupOfModule, PARAM_GROUPS, plainKey } from "@/lib/parameters";
import { coins, Fields, humanise } from "@/render/messages/common";
import { coinsText, type Action, type Entry } from "@/render/proposals";

// Custom breakpoints are not in Chakra's generated types, so responsive objects using them are hoisted.
// A spend's transfer: module → amount → recipient in one row from c2, stacked on phones.
const TRANSFER = { base: "minmax(0, 1fr)", c2: "auto auto 20px minmax(0, 1fr)" };

const BECH32 = /^bze(valoper)?1[02-9ac-hj-np-z]{38,}$/;

/** A raw value: an address as a chip, coins in display units, text as is, anything else as JSON. */
function Value({ value, denoms }: { value: unknown; denoms?: Map<string, TokenSummary> }) {
  if (typeof value === "string" && BECH32.test(value)) {
    return value.startsWith("bzevaloper1") ? <ValidatorChip operator={value} /> : <AccountChip address={value} />;
  }
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return (
      <Text as="span" wordBreak="break-word">
        {String(value)}
      </Text>
    );
  }
  const list = coins(Array.isArray(value) ? value : [value]);
  if (list.length > 0 && list.length === (Array.isArray(value) ? value.length : 1)) {
    return <Text as="span">{coinsText(list, denoms)}</Text>;
  }
  return (
    <Box as="pre" fontFamily="mono" fontSize="xs" whiteSpace="pre-wrap" wordBreak="break-all">
      {JSON.stringify(value, null, 2)}
    </Box>
  );
}

function EntryTable({ entries, denoms }: { entries: Entry[]; denoms?: Map<string, TokenSummary> }) {
  return <Fields rows={entries.map((e) => ({ label: humanise(e.key), value: <Value value={e.value} denoms={denoms} /> }))} />;
}

/** A parameter change's new values: plain names and values from the parameters dictionary, raw for keys it lacks. */
function ParamTable({ module, entries, denoms }: { module: string; entries: Entry[]; denoms?: Map<string, TokenSummary> }) {
  return (
    <Fields
      rows={entries.map((e) => {
        const plain = describeParam(module, e.key, e.value);
        return {
          label: plain?.name ?? plainKey(e.key),
          value: plain ? (
            <Text as="span">
              {plain.value}{" "}
              <Text as="span" fontFamily="mono" fontSize="xs" color="fg.subtle">
                {e.key}
              </Text>
            </Text>
          ) : (
            <Value value={e.value} denoms={denoms} />
          ),
        };
      })}
    />
  );
}

/** The community pool's module account (the distribution module's) as a chip; a plain module chip until `/modules` answers. */
function CommunityPool() {
  const modules = useModules();
  const account = modules.data?.find((m) => m.module === "distribution");
  if (!account) return <Chip variant="modc">Community pool module</Chip>;
  return <AccountChip address={account.address} label={{ name: "Community pool module", kind: "module" }} />;
}

const blockLink = (height: number) => (
  <Link asChild color="accent.fg" fontFamily="mono">
    <NextLink href={`/blocks/${height}`}>{formatCount(height)}</NextLink>
  </Link>
);

interface ActionProps {
  a: Action;
  p: Proposal;
  denoms?: Map<string, TokenSummary>;
  /** The upgrade's estimated date, null once reached or unknown. */
  upgradeEta: Date | null;
}

/** One action of the proposal in plain words. */
export function ActionView({ a, p, denoms, upgradeEta }: ActionProps) {
  switch (a.type) {
    case "upgrade":
      return (
        <Stack gap="2" fontSize="sm" data-testid="action" data-action={a.type}>
          <Text>
            Upgrades the chain software to{" "}
            <Text as="span" fontWeight="semibold">
              {a.name}
            </Text>
            {a.height != null && <> at block {blockLink(a.height)}</>}
            {upgradeEta && <>, expected around {formatDate(upgradeEta)}</>}.
          </Text>
          <Text color="fg.muted">
            Every validator must run the new version from that block. The chain pauses for a few minutes during the switch.
            Wallets, balances and tokens are not affected.
          </Text>
          <HStack gap="18px" wrap="wrap" fontSize="13px">
            <Link href={`https://github.com/bze-alphateam/bze/releases/tag/${encodeURIComponent(a.name)}`} target="_blank" rel="noopener noreferrer" color="accent.fg">
              Release notes on GitHub ↗
            </Link>
            <Link href={externalLinks.docs} target="_blank" rel="noopener noreferrer" color="accent.fg">
              How upgrades work, in the docs ↗
            </Link>
          </HStack>
        </Stack>
      );
    case "cancel_upgrade":
      return (
        <Text fontSize="sm" data-testid="action" data-action={a.type}>
          Cancels the software upgrade planned by an earlier proposal.
        </Text>
      );
    case "spend":
      return (
        <Stack gap="2" fontSize="sm" data-testid="action" data-action={a.type}>
          <Grid
            templateColumns={TRANSFER}
            justifyContent="start"
            alignItems="center"
            columnGap="3"
            rowGap="1"
            px="3"
            py="10px"
            rounded="l2"
            borderWidth="1px"
            borderColor="border.panel"
            bg="bg.page"
            data-testid="funds-box"
          >
            <Box minW="0">
              <CommunityPool />
            </Box>
            <Text as="span" fontFamily="mono" fontWeight="500">
              {coinsText(a.amount, denoms)}
            </Text>
            <Text as="span" hideBelow="c2" color="fg.muted" textAlign="center" aria-hidden>
              →
            </Text>
            <Box minW="0">{a.recipient ? <AccountChip address={a.recipient} /> : <Text as="span">an unknown account</Text>}</Box>
          </Grid>
          {p.status === "passed" && p.resolved_height != null && (
            <Text color="fg.muted" data-testid="spend-done">
              Done by the chain in block {blockLink(p.resolved_height)}, right when voting ended. There is no transaction for it:
              look for it under &ldquo;Where the funds went&rdquo; on that block.
            </Text>
          )}
        </Stack>
      );
    case "params": {
      const group = PARAM_GROUPS.find((g) => g.group === groupOfModule(a.module));
      return (
        <Stack gap="3" fontSize="sm" data-testid="action" data-action={a.type}>
          <Text>
            {a.whole ? "Sets all" : "Changes"} {a.entries.length} {a.module}{" "}
            {a.entries.length === 1 ? "parameter" : "parameters"}
            {a.whole ? ", to these values:" : ":"}
          </Text>
          <ParamTable module={a.module} entries={a.entries} denoms={denoms} />
          <Text color="fg.muted">
            Takes effect the moment the proposal passes ·{" "}
            <Link asChild>
              <NextLink href={`/parameters?group=${group?.group ?? encodeURIComponent(a.module)}`}>
                see all {group?.label ?? a.module} parameters
              </NextLink>
            </Link>
          </Text>
        </Stack>
      );
    }
    case "publisher":
      return (
        <HStack gap="1.5" wrap="wrap" fontSize="sm" data-testid="action" data-action={a.type}>
          <Text as="span">
            {a.add ? "Adds" : "Removes"} {a.name ?? "a publisher"}
          </Text>
          {a.address && <AccountChip address={a.address} />}
          <Text as="span">{a.add ? "as a CoinTrunk publisher" : "from the CoinTrunk publishers"}</Text>
        </HStack>
      );
    case "domain":
      return (
        <Text fontSize="sm" data-testid="action" data-action={a.type}>
          {a.add ? "Adds" : "Removes"}{" "}
          <Text as="span" fontWeight="semibold">
            {a.domain ?? "a domain"}
          </Text>{" "}
          {a.add ? "to" : "from"} the domains CoinTrunk articles can link to.
        </Text>
      );
    case "ibc_client":
      return (
        <Text fontSize="sm" data-testid="action" data-action={a.type}>
          Repairs the connection to chain client{" "}
          <Text as="span" fontFamily="mono">
            {a.subject ?? "?"}
          </Text>
          {a.substitute && (
            <>
              , taking over the state of client{" "}
              <Text as="span" fontFamily="mono">
                {a.substitute}
              </Text>
            </>
          )}
          . Transfers over it work again once the proposal passes.
        </Text>
      );
    case "text":
      return null;
    case "other":
      return (
        <Stack gap="2" data-testid="action" data-action={a.type}>
          <Text fontWeight="semibold" fontSize="sm">
            {a.title}
          </Text>
          {a.entries.length > 0 ? (
            <EntryTable entries={a.entries} denoms={denoms} />
          ) : (
            <Text fontSize="sm" color="fg.muted">
              No details.
            </Text>
          )}
        </Stack>
      );
  }
}
