"use client";

import { Box, Container, Flex, Text } from "@chakra-ui/react";
import BigNumber from "bignumber.js";
import type { ReactNode } from "react";

import { useStats } from "@/api/hooks";
import type { Stats } from "@/api/types";
import { Tag } from "@/components/ui/status-pill";
import { env } from "@/lib/env";
import { formatCount, formatPct, formatUsd } from "@/lib/format";

/** The 24h price change as `▲6.5%` / `▼1.2%`, and which way it went. */
function priceMove(pct: string | null): { text: string; tone: "pos.fg" | "neg.fg" | "fg.muted" } | null {
  if (pct == null) return null;
  const v = new BigNumber(pct);
  if (!v.isFinite()) return null;
  const text = formatPct(v.abs().toFixed());
  if (text === "0.0%") return { text, tone: "fg.muted" };
  return v.gt(0) ? { text: `▲${text}`, tone: "pos.fg" } : { text: `▼${text}`, tone: "neg.fg" };
}

function Item({ label, children, hideBelow }: { label: string; children: ReactNode; hideBelow?: "c2" | "c3" }) {
  return (
    <Text as="span" color="fg.muted" whiteSpace="nowrap" hideBelow={hideBelow} data-item={label}>
      {label}{" "}
      <Text as="b" fontFamily="mono" fontWeight="500" color="fg">
        {children}
      </Text>
    </Text>
  );
}

const or = <T,>(value: T | null | undefined, show: (v: T) => string) => (value == null ? "—" : show(value));

/**
 * The quiet network summary above the header, from `/stats`: price, height,
 * block time; on wider screens the day's transactions and the staking figures.
 */
export function NetworkStrip() {
  const s: Stats | undefined = useStats().data;
  const move = priceMove(s?.price_change_24h_pct ?? null);
  return (
    <Box as="aside" aria-label="Network summary" bg="bg.page" borderBottomWidth="1px" borderColor="border.panel" fontSize="12px">
      <Container maxW="7xl" px={{ base: "4", md: "6" }}>
        <Flex minH="32px" align="center" gap="22px" wrap="wrap">
          <Tag tone="accent" fontSize="10px" px="6px" py="1px">
            {env.networkLabel}
          </Tag>
          <Item label="BZE">
            {or(s?.price_usd, (p) => formatUsd(p).replace(/^≈ /, ""))}
            {move && (
              <Box as="span" color={move.tone} ms="1">
                {move.text}
              </Box>
            )}
          </Item>
          <Item label="Height">{or(s?.latest_height, formatCount)}</Item>
          <Item label="Block time">{or(s?.avg_block_time_ms, (ms) => `${(ms / 1000).toFixed(1)}s`)}</Item>
          <Item label="Txs 24h" hideBelow="c2">
            {or(s?.txs_24h, formatCount)}
          </Item>
          <Item label="Staked" hideBelow="c2">
            {or(s?.staked_share, (v) => formatPct(v))}
          </Item>
          <Item label="Reward rate" hideBelow="c3">
            {or(s?.reward_rate, (v) => `≈${formatPct(v, { trim: true })}`)}
          </Item>
          <Item label="Validators" hideBelow="c3">
            {s?.validators && s.max_validators != null ? `${s.validators.active}/${s.max_validators}` : "—"}
          </Item>
        </Flex>
      </Container>
    </Box>
  );
}
