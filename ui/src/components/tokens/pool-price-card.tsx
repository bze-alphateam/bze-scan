"use client";

import { Box, Flex, Popover, Portal, Text } from "@chakra-ui/react";
import { LuCircleHelp } from "react-icons/lu";

import type { Token } from "@/api/types";
import { AppLink } from "@/components/layout/app-link";
import { DetailList, DetailRow } from "@/components/ui/detail-list";
import { ListPanel } from "@/components/ui/list";
import { PanelHead } from "@/components/ui/page-head";
import { formatPct } from "@/lib/format";
import { dexAssetUrl } from "@/render/account";
import { poolPriceText, poolPriceUnit, poolStatus, poolTicker } from "@/render/pool-price";

const TONE_COLOR = { warn: "warn.fg", ok: "pos.fg", muted: "fg.muted" } as const;

/** The "?" next to the title: why the chain compares the pool price with its average. */
function WhyHelp() {
  return (
    <Popover.Root positioning={{ placement: "top" }} lazyMount unmountOnExit>
      <Popover.Trigger asChild>
        <Box asChild cursor="help" color="fg.muted" display="inline-flex" ml="2">
          <button type="button" aria-label="Why the pool price is compared with its average" data-testid="pool-price-help">
            <LuCircleHelp size={14} />
          </button>
        </Box>
      </Popover.Trigger>
      <Portal>
        <Popover.Positioner>
          <Popover.Content maxW="xs">
            <Popover.Arrow />
            <Popover.Body>
              <Text fontSize="sm">
                The chain keeps a moving average of every pool&apos;s price. When the pool price jumps away from it, the chain stops using the pool for fees and
                burns, so a manipulated price cannot be used to drain them. It comes back by itself once the price settles.
              </Text>
            </Popover.Body>
          </Popover.Content>
        </Popover.Positioner>
      </Portal>
    </Popover.Root>
  );
}

/** The "Pool price" card of an LP token: the current price, its recent average, the drift and what it means. Nothing without a pool. */
export function PoolPriceCard({ t }: { t: Token }) {
  const pool = t.pool;
  if (!pool) return null;
  const price = poolPriceText(pool, "price");
  const avg = poolPriceText(pool, "avg_price");
  const unit = poolPriceUnit(pool);
  const lines = poolStatus(pool);
  const halfLife = pool.half_life_blocks != null ? ` (half-life ${pool.half_life_blocks} blocks)` : "";
  return (
    <Box mt="8" data-testid="pool-price">
      <ListPanel>
        <PanelHead id="pool-price-title" title="Pool price" note={<WhyHelp />} />
        <DetailList>
          <DetailRow label="Current price">
            <Text as="span" fontFamily="mono" data-testid="pool-price-now">
              {price ?? "—"}
            </Text>
            <Text as="span" color="fg.muted" fontSize="12px">
              {price ? unit : "the pool is empty"}
            </Text>
          </DetailRow>
          <DetailRow label={`Recent average${halfLife}`}>
            <Text as="span" fontFamily="mono" data-testid="pool-price-avg">
              {avg ?? "—"}
            </Text>
            {pool.drift_pct != null && (
              <Text as="span" color="fg.muted" fontSize="12px" data-testid="pool-price-drift">
                {formatPct(pool.drift_pct)} away
              </Text>
            )}
          </DetailRow>
          <DetailRow label="Status" last>
            <Flex direction="column" gap="1" data-testid="pool-price-status">
              {lines.length === 0 ? (
                <Text color="fg.muted">No fee rule applies to a pool without BZE.</Text>
              ) : (
                lines.map((l) => (
                  <Text key={l.key} color={TONE_COLOR[l.tone]} data-testid={`pool-status-${l.key}`}>
                    {l.text}
                  </Text>
                ))
              )}
            </Flex>
          </DetailRow>
        </DetailList>
        <Box px="4" py="3" borderTopWidth="1px" borderColor="border.panel" fontSize="sm">
          <Text as="span" color="fg.muted" data-testid="pool-price-trade">
            Trade {poolTicker(pool)} on{" "}
            <AppLink app="dex" href={dexAssetUrl(poolTradeDenom(t))}>
              DEX
            </AppLink>
          </Text>
        </Box>
      </ListPanel>
    </Box>
  );
}

/** The pool's token that is not BZE: where the DEX page of the pool starts. */
function poolTradeDenom(t: Token): string {
  const pool = t.pool;
  if (!pool) return t.denom;
  const { base, quote } = pool.reserves;
  return base.denom === "ubze" ? quote.denom : base.denom;
}
