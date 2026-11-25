"use client";

import { Text } from "@chakra-ui/react";

import type { BlockUpgrade } from "@/api/types";
import { UpgradeRecovery } from "@/components/proposals/upgrade-recovery";
import { Card } from "@/components/ui/card";
import { HashLink } from "@/components/ui/links";
import { emergencyLine } from "@/render/halt";

import { LiveHaltPanel } from "./live-halt";

/**
 * On the page of block `height`, the emergency halt planned there (chain
 * v8.3.0: the guardian or governance stops the chain at the next block, no
 * proposal behind it): who scheduled it why, the live halt while the chain
 * waits, and when each validator came back once the block is indexed.
 */
export function EmergencyHaltNote({
  height,
  upgrade,
  indexed,
}: {
  height: number;
  upgrade: Pick<BlockUpgrade, "reason" | "scheduled_by"> & { scheduled_tx_hash?: string | null };
  /** The block is indexed: the halt is over or being read, the recovery block shows. */
  indexed: boolean;
}) {
  return (
    <Card title="Emergency halt" borderColor="warn.fg" mt="6" data-testid="emergency-halt">
      <Text fontWeight="600" data-testid="emergency-halt-line">
        {emergencyLine({ height, reason: upgrade.reason, scheduled_by: upgrade.scheduled_by })}
      </Text>
      <Text fontSize="13px" color="fg.muted" mt="1">
        The chain stopped after this block until enough validators restarted it. No proposal is behind an emergency halt.
      </Text>
      {upgrade.scheduled_tx_hash && (
        <Text fontSize="13px" color="fg.muted" mt="1" as="div">
          Scheduled in transaction <HashLink hash={upgrade.scheduled_tx_hash} noCopy fontSize="13px" />
        </Text>
      )}
      <LiveHaltPanel height={height} mt="4" />
      {indexed && <UpgradeRecovery upgrade={{ height }} title="After the halt" />}
    </Card>
  );
}
