"use client";

import { Box, Flex, Link, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useMemo, useState } from "react";
import { LuChevronDown, LuChevronRight } from "react-icons/lu";

import { useAccountOpenOrders, useDenoms } from "@/api/hooks";
import { AppLink } from "@/components/layout/app-link";
import { TokenLogo } from "@/components/tokens/tokens-page";
import { Amount } from "@/components/ui/amount";
import { ListHead, ListPanel, ListRow } from "@/components/ui/list";
import { TableSkeleton } from "@/components/ui/skeleton";
import { formatCount, formatUsd } from "@/lib/format";
import { marketsText, orderRows, ordersText, ordersTotalUsd, type OrderAssetRow, type OrderMarketRow } from "@/render/account";
import { dexMarketUrl, marketName } from "@/render/messages/dex";
import { createRenderContext } from "@/render/messages/registry";
import type { RenderContext } from "@/render/messages/types";
import { tokenHref } from "@/render/tokens";

// Asset · In orders · Value (c2) · Orders (c2) · expand. On phones the value
// sits under the amount and the counts under the symbol, as in the Tokens tab.
// From c2 no column may be `auto`: every row is its own grid.
const COLUMNS = {
  base: "minmax(0, 1fr) minmax(0, 1fr) 44px",
  c2: "minmax(0, 1.2fr) minmax(0, 1.2fr) minmax(0, 1fr) minmax(0, 1.3fr) 44px",
};

const NO_TRANSFERS: never[] = [];

function ValueText({ value }: { value: string | null }) {
  return value == null ? (
    <Text as="span" color="fg.muted">
      no price
    </Text>
  ) : (
    <>{formatUsd(value)}</>
  );
}

function AssetAmount({ row, amount }: { row: OrderAssetRow; amount: string }) {
  return <Amount amount={amount} denom={row.denom} symbol={row.symbol} exponent={row.exponent} />;
}

function MarketRow({ row, market, ctx }: { row: OrderAssetRow; market: OrderMarketRow; ctx: RenderContext }) {
  const counts = (
    <>
      <Text as="span" color="fg.muted">
        {ordersText(market.ordersCount)}
      </Text>
      <AppLink app="dex" href={dexMarketUrl(market.marketId)}>
        DEX
      </AppLink>
    </>
  );
  return (
    <ListRow
      role="listitem"
      templateColumns={COLUMNS}
      rowGap="1"
      bg="bg.subtle"
      py="2"
      borderTopWidth="1px"
      borderBottomWidth="0"
      data-testid="order-market-row"
    >
      <Text pl="6" minW="0" truncate title={market.marketId} data-testid="order-market">
        {marketName(market.marketId, ctx)}
      </Text>
      <Box textAlign="right" minW="0">
        <Text fontFamily="mono" fontSize="13px" wordBreak="break-word">
          <AssetAmount row={row} amount={market.amount} />
        </Text>
        <Text hideFrom="c2" fontFamily="mono" fontSize="12px" color="fg.muted">
          <ValueText value={market.valueUsd} />
        </Text>
        <Flex hideFrom="c2" justify="flex-end" align="center" gap="2" fontSize="12px" mt="1">
          {counts}
        </Flex>
      </Box>
      <Text hideBelow="c2" textAlign="right" fontFamily="mono" fontSize="13px" whiteSpace="nowrap">
        <ValueText value={market.valueUsd} />
      </Text>
      <Flex hideBelow="c2" justify="flex-end" align="center" gap="3" fontSize="13px">
        {counts}
      </Flex>
      <Box />
    </ListRow>
  );
}

function AssetRow({ row, open, onToggle, ctx }: { row: OrderAssetRow; open: boolean; onToggle: () => void; ctx: RenderContext }) {
  const denoms = useDenoms();
  const name = row.symbol ?? row.denom;
  const counts = `${ordersText(row.ordersCount)} · ${marketsText(row.markets.length)}`;
  return (
    <Box
      role="listitem"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      data-testid="order-asset-row"
    >
      <ListRow templateColumns={COLUMNS} rowGap="2" borderBottomWidth="0">
        <Flex align="center" gap="10px" minW="0">
          <TokenLogo t={{ denom: row.denom, symbol: row.symbol, logo_url: row.logoUrl, lp_pair: row.lpPair }} denoms={denoms.data} />
          <Box minW="0">
            {row.token ? (
              <Link asChild display="block" fontWeight="500" color="accent.fg" truncate>
                <NextLink href={tokenHref(row.denom)} title={row.denom} data-testid="order-symbol">
                  {name}
                </NextLink>
              </Link>
            ) : (
              <Text fontWeight="500" truncate title={row.denom} data-testid="order-symbol">
                {name}
              </Text>
            )}
            <Text hideFrom="c2" fontSize="12px" color="fg.muted">
              {counts}
            </Text>
          </Box>
        </Flex>
        <Box textAlign="right" minW="0">
          <Text fontFamily="mono" fontSize="13px" wordBreak="break-word" data-testid="order-amount">
            <AssetAmount row={row} amount={row.amount} />
          </Text>
          <Text hideFrom="c2" fontFamily="mono" fontSize="12px" color="fg.muted">
            <ValueText value={row.valueUsd} />
          </Text>
        </Box>
        <Text hideBelow="c2" textAlign="right" fontFamily="mono" fontSize="13px" whiteSpace="nowrap" data-testid="order-value">
          <ValueText value={row.valueUsd} />
        </Text>
        <Text hideBelow="c2" textAlign="right" fontSize="13px" color="fg.muted" data-testid="order-counts">
          {counts}
        </Text>
        <Flex
          as="button"
          {...{ type: "button" }}
          aria-expanded={open}
          aria-label={`${open ? "Hide" : "Show"} the markets holding ${name}`}
          onClick={onToggle}
          boxSize="44px"
          align="center"
          justify="center"
          rounded="l2"
          borderWidth="1px"
          borderColor="border.panel"
          bg="bg.subtle"
          color="fg"
          cursor="pointer"
          _hover={{ bg: "bg.hover" }}
          data-testid="order-toggle"
        >
          {open ? <LuChevronDown aria-hidden size={18} /> : <LuChevronRight aria-hidden size={18} />}
        </Flex>
      </ListRow>
      {open && (
        <Box role="list" aria-label={`Markets holding ${name}`}>
          {row.markets.map((m) => (
            <MarketRow key={m.marketId} row={row} market={m} ctx={ctx} />
          ))}
        </Box>
      )}
    </Box>
  );
}

/**
 * The tokens a personal account has locked in open DEX orders, per asset,
 * each expanding to the markets holding it. Its own request: a slow or
 * failed answer changes this panel only.
 */
export function OrdersTab({ address }: { address: string }) {
  const orders = useAccountOpenOrders(address);
  const denoms = useDenoms();
  const [toggled, setToggled] = useState<Record<string, boolean>>({});
  const ctx = useMemo(() => createRenderContext({ transfers: NO_TRANSFERS, tokens: denoms.data }), [denoms.data]);
  if (!orders.data && orders.error) {
    return (
      <Text fontSize="sm" color="fg.muted" data-testid="orders-error">
        The open orders could not be loaded right now.
      </Text>
    );
  }
  // Without the tokens list the rows still render, unpriced, once it failed.
  if (!orders.data || (!denoms.data && !denoms.error)) return <TableSkeleton rows={3} columns={5} />;
  const o = orders.data;
  if (o.assets.length === 0) {
    return (
      <ListPanel px="4" py="6" data-testid="orders-empty">
        <Text color="fg.muted" fontSize="sm">
          This account has no open orders on the DEX.
        </Text>
      </ListPanel>
    );
  }
  const rows = orderRows(o, denoms.data);
  const { total, partial } = ordersTotalUsd(rows);
  const single = rows.length === 1;
  return (
    <Box>
      <Flex justify="space-between" align="baseline" gap="3" wrap="wrap" mb="14px" fontSize="13px">
        <Text color="fg.muted" data-testid="orders-summary">
          Locked in {formatCount(o.orders_count)} open {o.orders_count === 1 ? "order" : "orders"} · {marketsText(o.markets_count)}
        </Text>
        <Text color="fg.muted" data-testid="orders-total">
          <Text as="span" fontFamily="mono">
            {formatUsd(total)}
          </Text>
          {partial && " of the priced tokens"}
        </Text>
      </Flex>
      <ListPanel>
        <ListHead templateColumns={COLUMNS} aria-hidden>
          <Text>Asset</Text>
          <Text textAlign="right">In orders</Text>
          <Text hideBelow="c2" textAlign="right">
            Value
          </Text>
          <Text hideBelow="c2" textAlign="right">
            Orders
          </Text>
          <Text />
        </ListHead>
        <Box role="list" aria-label="Locked per asset">
          {rows.map((row) => {
            const open = toggled[row.denom] ?? single;
            return (
              <AssetRow
                key={row.denom}
                row={row}
                open={open}
                onToggle={() => setToggled((t) => ({ ...t, [row.denom]: !open }))}
                ctx={ctx}
              />
            );
          })}
        </Box>
      </ListPanel>
    </Box>
  );
}
