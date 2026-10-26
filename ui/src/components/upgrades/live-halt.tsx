"use client";

import { Box, Flex, Grid, Text } from "@chakra-ui/react";
import type BigNumber from "bignumber.js";

import { useLiveHalt } from "@/api/hooks";
import type { Halted, LiveHaltMember } from "@/api/types";
import { Card } from "@/components/ui/card";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatCount, formatPct, shortHash } from "@/lib/format";
import { backLine, haltedFor, haltProgress, missingMembers, NEEDED_PCT, sharePct } from "@/render/halt";

/**
 * While the chain is halted for a software upgrade: how much voting power is
 * back on the new version against the two thirds it needs, and the validators
 * still missing. Renders nothing otherwise, or when `proposalId` names
 * another upgrade's proposal.
 */
export function LiveHaltPanel({ proposalId, ...props }: { proposalId?: number; mt?: string; mb?: string }) {
  const h = useLiveHalt().data;
  if (!h?.halted || (proposalId != null && h.proposal_id !== proposalId)) return null;
  return <Panel h={h} {...props} />;
}

function Panel({ h, ...props }: { h: Halted; mt?: string; mb?: string }) {
  const p = haltProgress(h);
  const missing = missingMembers(h);
  return (
    <Card
      title="Chain paused for the upgrade"
      meta={`Waiting at block ${formatCount(h.halt_height)} for ${haltedFor(h, new Date())}, round ${h.round}. Updates every few seconds.`}
      borderColor="warn.fg"
      data-testid="live-halt"
      {...props}
    >
      <Text fontWeight="600" mb="3" data-testid="live-halt-line">
        {backLine(p)}.
      </Text>
      <Bar label="Back on the new version" pct={p.back} main />
      <Bar label={`Prevotes, round ${h.round}`} pct={p.prevote} />
      <Bar label={`Precommits, round ${h.round}`} pct={p.precommit} />
      <Text fontSize="12px" color="fg.muted" mt="2">
        The chain restarts once more than {formatPct(NEEDED_PCT.toString())} of the voting power precommits a block.
      </Text>
      {missing.length > 0 && (
        <Box mt="4">
          <Flex justify="space-between" py="2" borderBottomWidth="1px" borderColor="border.panel" textStyle="tableHead">
            <Text>Not back yet ({missing.length})</Text>
            <Text>Voting power</Text>
          </Flex>
          <Box role="list" aria-label="Validators not back yet">
            {missing.map((m) => (
              <MissingRow key={m.consensus_address} m={m} total={h.total_power} />
            ))}
          </Box>
        </Box>
      )}
    </Card>
  );
}

function Bar({ label, pct, main = false }: { label: string; pct: BigNumber; main?: boolean }) {
  const reached = pct.gt(NEEDED_PCT);
  const width = `${Math.min(100, pct.toNumber())}%`;
  return (
    <Box mb="2.5" data-testid="live-halt-bar" data-label={label}>
      <Flex justify="space-between" fontSize="12px" mb="1">
        <Text color={main ? "fg" : "fg.muted"} fontWeight={main ? "600" : "400"}>
          {label}
        </Text>
        <Text fontFamily="mono" color={reached ? "pos.fg" : "fg"}>
          {formatPct(pct.toString())}
        </Text>
      </Flex>
      <Box position="relative" h={main ? "10px" : "6px"} rounded="full" bg="bg.hover" aria-hidden>
        <Box h="full" w={width} rounded="full" bg={reached ? "pos.fg" : "warn.fg"} />
        <Box position="absolute" top="-3px" bottom="-3px" left={`${NEEDED_PCT.toNumber()}%`} w="2px" bg="fg" opacity={0.6} />
      </Box>
    </Box>
  );
}

function MissingRow({ m, total }: { m: LiveHaltMember; total: string }) {
  return (
    <Grid
      role="listitem"
      templateColumns="minmax(0, 1fr) auto"
      gap="3"
      alignItems="center"
      py="8px"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      data-testid="live-halt-missing"
    >
      <Box minW="0">
        {m.operator_address ? (
          <ValidatorChip operator={m.operator_address} moniker={m.moniker} />
        ) : (
          <Text as="span" fontFamily="mono" title={m.consensus_address}>
            {shortHash(m.consensus_address)}
          </Text>
        )}
      </Box>
      <Text fontFamily="mono" color="fg.muted">
        {formatPct(sharePct(m.voting_power, total).toString())}
      </Text>
    </Grid>
  );
}
