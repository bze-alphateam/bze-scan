"use client";

import { Box, Grid, Heading, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useId } from "react";

import { useUpgrade } from "@/api/hooks";
import type { Proposal, ProposalUpgrade, Upgrade, UpgradeMember } from "@/api/types";
import { ListSkeleton } from "@/components/ui/skeleton";
import { ValidatorChip } from "@/components/ui/validator-chip";
import { formatPct, shortHash } from "@/lib/format";
import { recoveryHeader, recoveryText, type RecoveryTone } from "@/render/proposals/recovery";

// Validator · Share of stake (c2) · Back.
const MEMBER = { base: "minmax(0, 1fr) auto", c2: "minmax(0, 1fr) 110px minmax(0, 1.2fr)" };

const TONES: Record<RecoveryTone, string> = { normal: "fg", muted: "fg.muted", neg: "neg.fg" };

/** The upgrade of a passed software upgrade proposal once the chain halted for it; null for anything else. */
export function haltedUpgrade(p: Proposal): ProposalUpgrade | null {
  if (p.status !== "passed" || p.kind !== "software_upgrade" || !p.upgrade) return null;
  return p.upgrade.halt_time != null ? p.upgrade : null;
}

/** "After the upgrade": when each validator of the set at the halt came back, in arrival order. */
export function UpgradeRecovery({ upgrade }: { upgrade: ProposalUpgrade }) {
  const id = useId();
  const q = useUpgrade(upgrade.height);
  return (
    <Box as="section" aria-labelledby={id} mt="5" pt="4" borderTopWidth="1px" borderColor="border.panel" data-testid="upgrade-recovery">
      <Heading as="h3" id={id} fontSize="14px" fontWeight="600" mb="1">
        After the upgrade
      </Heading>
      {q.data ? (
        <Recovery u={q.data} />
      ) : q.error ? (
        <Text color="fg.muted">When the validators came back could not be loaded.</Text>
      ) : (
        <ListSkeleton rows={5} />
      )}
    </Box>
  );
}

function Recovery({ u }: { u: Upgrade }) {
  const header = recoveryHeader(u);
  const pending = u.computed_at == null;
  return (
    <>
      {header && (
        <Text color="fg.muted" data-testid="recovery-header">
          The chain halted at block{" "}
          <Link asChild color="accent.fg">
            <NextLink href={`/blocks/${u.height}`}>{header.height}</NextLink>
          </Link>{" "}
          on {header.halt} and restarted {header.downtime} later.
        </Text>
      )}
      {!u.watched && (
        <Text color="fg.muted" fontSize="12px" mt="1" data-testid="recovery-unwatched">
          The explorer was not watching during this halt, so validators back before the restart show no exact time.
        </Text>
      )}
      {pending && (
        <Text color="fg.muted" fontSize="12px" mt="1" data-testid="recovery-pending">
          Still collecting
        </Text>
      )}
      {u.members.length > 0 && (
        <>
          <Grid templateColumns={MEMBER} gap="3" mt="3" py="2" borderBottomWidth="1px" borderColor="border.panel" textStyle="tableHead" aria-hidden>
            <Text>Validator</Text>
            <Text hideBelow="c2" textAlign="end">
              Share of stake
            </Text>
            <Text textAlign="end">Back</Text>
          </Grid>
          <Box role="list" aria-label="When the validators came back">
            {u.members.map((m) => (
              <MemberRow key={m.consensus_address} m={m} u={u} />
            ))}
          </Box>
        </>
      )}
    </>
  );
}

function MemberRow({ m, u }: { m: UpgradeMember; u: Upgrade }) {
  const back = recoveryText(m, u);
  return (
    <Grid
      role="listitem"
      templateColumns={MEMBER}
      gap="3"
      alignItems="center"
      py="9px"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      data-testid="recovery-row"
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
      <Text hideBelow="c2" textAlign="end" fontFamily="mono" color="fg.muted">
        {formatPct(m.power_pct)}
      </Text>
      <Text textAlign="end" color={TONES[back.tone]} data-testid="recovery-back">
        {back.text}
      </Text>
    </Grid>
  );
}
