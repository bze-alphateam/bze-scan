import { Box, HStack, Link, Stack, Text } from "@chakra-ui/react";
import BigNumber from "bignumber.js";
import NextLink from "next/link";
import { LuClock } from "react-icons/lu";

import { AppLink } from "@/components/layout/app-link";
import type { Coin, Order, Refund, SkippedCancel, Transfer } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { shortOrderId } from "@/lib/format";

import { Account, accountName, coin, coinText, Coins, Fields, obj, str, transfersOf } from "./common";
import {
  dexMarketUrl,
  fillTotals,
  filledPct,
  marketCoins,
  marketName,
  orderSide,
  outcomeOf,
  priceText,
  refundTotals,
  returnedCoin,
  splitMarket,
  type MessageOutcome,
} from "./dex";
import { renderer, type BodyProps, type MessageRenderer, type RenderContext, type RenderMessage } from "./types";

const DEX_MODULE = "DEX";

/** Whether an address is the DEX module, as the transaction's labels name it. */
const isDex = (address: string | null, ctx: RenderContext) => address != null && ctx.label(address)?.name === DEX_MODULE;

/** `1,000 BZE`, from base units of a market's base denom. */
function baseText(marketId: string | null, amount: string | null, ctx: RenderContext): string {
  const m = splitMarket(marketId);
  if (!m || amount == null) return amount ?? "an unknown amount";
  return coinText({ denom: m.base, amount }, ctx);
}

/** `1,000 BZE at 0.0125 USDC`. */
function orderText(marketId: string | null, amount: string | null, price: string | null, ctx: RenderContext): string {
  return `${baseText(marketId, amount, ctx)} at ${priceText(price, marketId, ctx)}`;
}

const sideWord = (side: string | null) => (orderSide(side) === "sell" ? "Sell" : "Buy");

/** "The order book processes this at the end of the block". */
function PendingLine() {
  return (
    <HStack gap="2" color="orange.fg" fontSize="sm" align="start" role="status" data-testid="dex-pending">
      <Box as="span" display="inline-flex" mt="0.5" aria-hidden>
        <LuClock />
      </Box>
      <Text>The order book processes this at the end of the block; the outcome will appear here.</Text>
    </HStack>
  );
}

/** A message the order book failed to process (chain v8.3.0): it stays queued and is retried. */
function FailedLine() {
  return (
    <HStack gap="2" color="orange.fg" fontSize="sm" align="start" role="status" data-testid="dex-failed">
      <Box as="span" display="inline-flex" mt="0.5" aria-hidden>
        <LuClock />
      </Box>
      <Text>The order book could not process this yet; it retries every block.</Text>
    </HStack>
  );
}

/** "16,087 BZE sent back", or that it was too small to send. */
function sentBackText(refunds: Refund[], ctx: RenderContext): string {
  const t = refundTotals(refunds);
  const coins = t.coins.map((c) => coinText(c, ctx)).join(" and ");
  if (coins && t.dust) return `${coins} sent back, the rest too small to send back`;
  return coins ? `${coins} sent back` : "too small to send back, kept as dust";
}

/** A create's remainder that could not rest (chain v8.3.0). */
function BelowMinLine({ refunds, ctx }: { refunds: Refund[]; ctx: RenderContext }) {
  return (
    <Text fontSize="sm" data-testid="dex-below-min">
      The rest was below the minimum order: {sentBackText(refunds, ctx)}.
    </Text>
  );
}

/** A cancel that found its order gone (chain v8.3.0). */
function SkippedCancelLine({ c }: { c: SkippedCancel }) {
  return (
    <Text fontSize="sm" data-testid="dex-cancel-skipped">
      Order <OrderId id={c.order_id} /> was already filled or cancelled; nothing to cancel.
    </Text>
  );
}

/** The makers of a few fills as chips; "N sellers" when many. */
function Makers({ makers, ctx }: { makers: string[]; ctx: RenderContext }) {
  const unique = [...new Set(makers)];
  if (unique.length > 3) {
    return (
      <Text as="span" color="fg.muted">
        {unique.length} orders on the book
      </Text>
    );
  }
  return (
    <HStack as="span" gap="1.5" wrap="wrap" display="inline-flex">
      {unique.map((m) => (
        <AccountChip key={m} address={m} label={ctx.label(m)} />
      ))}
    </HStack>
  );
}

/** `#0…6714623`, the full id on hover. */
function OrderId({ id }: { id: string }) {
  return (
    <Text as="span" fontFamily="mono" title={id} data-testid="order-id">
      #{shortOrderId(id)}
    </Text>
  );
}

function orderLink(o: Order) {
  return (
    <Link asChild>
      <NextLink href={`/accounts/${encodeURIComponent(o.owner)}?tab=activity`}>
        order <OrderId id={o.order_id} />
      </NextLink>
    </Link>
  );
}

/** One placed order's line: what is still on the book, or what became of it. */
function PlacedLine({ o, ctx }: { o: Order; ctx: RenderContext }) {
  const rest = BigNumber.max(new BigNumber(o.amount).minus(o.filled_amount), 0).toFixed();
  const text =
    o.status === "open"
      ? `${baseText(o.market_id, rest, ctx)} still waiting on the order book`
      : o.status === "filled"
        ? `${baseText(o.market_id, o.amount, ctx)} rested on the order book, since filled`
        : `${baseText(o.market_id, rest, ctx)} rested on the order book, since cancelled`;
  return (
    <Text fontSize="sm" data-testid="dex-placed" data-status={o.status}>
      {text} · {orderLink(o)}
    </Text>
  );
}

/** "Order #id cancelled, 5 USDC returned". */
function CancelledLine({ o, ctx }: { o: Order; ctx: RenderContext }) {
  const back = returnedCoin(o);
  return (
    <Text fontSize="sm" data-testid="dex-cancelled">
      Order <OrderId id={o.order_id} /> cancelled
      {back && new BigNumber(back.amount).gt(0) ? `, ${coinText(back, ctx)} returned` : ""}
    </Text>
  );
}

/** "Filled right away: 600 BZE for 7.5 USDC" with the makers. */
function FillsLine({ outcome, marketId, ctx }: { outcome: MessageOutcome; marketId: string | null; ctx: RenderContext }) {
  const m = splitMarket(marketId);
  if (outcome.fills.length === 0 || !m) return null;
  const t = fillTotals(outcome.fills);
  return (
    <HStack gap="2" wrap="wrap" fontSize="sm" data-testid="dex-fills">
      <Text as="span">
        Filled right away: {coinText({ denom: m.base, amount: t.base }, ctx)} for {coinText({ denom: m.quote, amount: t.quote }, ctx)}
      </Text>
      <Text as="span" color="fg.muted">
        with
      </Text>
      <Makers makers={outcome.fills.map((f) => f.maker)} ctx={ctx} />
    </HStack>
  );
}

/**
 * What the order book did with a message, from the transaction's `dex`:
 * pending until processed, then the fills, the order left on the book and
 * the cancels. Nothing when the transaction has no outcome.
 */
export function DexOutcome({ msg, ctx, final = false }: BodyProps & { final?: boolean }) {
  const outcome = outcomeOf(msg, ctx);
  if (!outcome) return null;
  const market = str(msg, "market_id");
  // A fill message's refunds are its not-filled line (NotFilledLine); a create's say why its rest did not rest.
  const belowMin = outcome.refunds.filter((r) => r.reason === "below_min_amount");
  const empty =
    outcome.fills.length + outcome.placed.length + outcome.cancelled.length + belowMin.length + outcome.skippedCancels.length === 0;
  // Failed and retried (chain v8.3.0): the outcome is still to come, whatever else the transaction settled.
  const stuck = isStuck(outcome);
  // A final message's outcome is complete once settled, whatever the transaction's pending flag says.
  const pending = outcome.pending && (stuck || !(final && ctx.dexSettled));
  const waiting = stuck ? <FailedLine /> : <PendingLine />;
  if (pending && empty) return waiting;
  return (
    <Stack gap="1.5" data-testid="dex-outcome">
      <FillsLine outcome={outcome} marketId={market} ctx={ctx} />
      {outcome.placed.map((o) => (
        <PlacedLine key={o.order_id} o={o} ctx={ctx} />
      ))}
      {outcome.cancelled.map((o) => (
        <CancelledLine key={o.order_id} o={o} ctx={ctx} />
      ))}
      {outcome.skippedCancels.map((c) => (
        <SkippedCancelLine key={c.message_id} c={c} />
      ))}
      {belowMin.length > 0 && <BelowMinLine refunds={belowMin} ctx={ctx} />}
      {pending && waiting}
    </Stack>
  );
}

/** A message the order book failed to process and has not processed since. */
const isStuck = (outcome: MessageOutcome) => outcome.pending && outcome.failed.length > 0;

/** "60% filled", the bar and its label. */
function FilledBar({ pct }: { pct: number }) {
  return (
    <Stack gap="1" maxW="sm">
      <Box
        h="2"
        bg="bg.muted"
        rounded="full"
        overflow="hidden"
        role="progressbar"
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Filled"
      >
        <Box h="full" w={`${pct}%`} bg="green.solid" />
      </Box>
      <Text fontSize="xs" color="fg.muted" data-testid="dex-filled">
        {pct}% filled
      </Text>
    </Stack>
  );
}

/** How much of the message's amount the outcome filled: right away, plus what its resting order filled since. */
function createdPct(msg: RenderMessage, outcome: MessageOutcome | null): number | null {
  const amount = str(msg, "amount");
  if (!outcome || outcome.pending || amount == null) return null;
  const filled = new BigNumber(fillTotals(outcome.fills).base).plus(
    outcome.placed.reduce((sum, o) => sum.plus(o.filled_amount), new BigNumber(0)),
  );
  return filledPct(amount, filled.toFixed());
}

function DexLink({ marketId, ctx }: { marketId: string | null; ctx: RenderContext }) {
  if (!splitMarket(marketId)) return null;
  return (
    <Text fontSize="sm">
      Open {marketName(marketId, ctx)} on{" "}
      <AppLink app="dex" href={dexMarketUrl(marketId as string)}>
        DEX
      </AppLink>
    </Text>
  );
}

/**
 * The transfer that holds an order's funds: the signer's largest transfer to
 * the DEX in the denom the order spends (the quote to buy, the base to sell);
 * the order fee goes to the DEX too, usually in another denom.
 */
function heldTransfer(msg: RenderMessage, ctx: RenderContext, side: "buy" | "sell" | null): Transfer | null {
  const m = splitMarket(str(msg, "market_id"));
  if (!m || !side) return null;
  const denom = side === "buy" ? m.quote : m.base;
  const creator = str(msg, "creator");
  const candidates = transfersOf(msg, ctx).filter((t) => t.sender === creator && isDex(t.recipient, ctx) && t.denom === denom);
  return candidates.reduce<Transfer | null>((max, t) => (max && new BigNumber(max.amount).gte(t.amount) ? max : t), null);
}

/** A transfer's reason in an order message: held, returned, bought, paid, or the DEX fee. */
function orderReason(t: Transfer, msg: RenderMessage, ctx: RenderContext, side: "buy" | "sell" | null): string {
  const creator = str(msg, "creator");
  if (heldTransfer(msg, ctx, side)?.seq === t.seq) return `held for the ${side} order`;
  if (isDex(t.sender, ctx) && t.recipient === creator) {
    if (msg.type_url.endsWith("MsgCancelOrder")) return "returned from the cancelled order";
    const makers = [...new Set((outcomeOf(msg, ctx)?.fills ?? []).map((f) => f.maker))];
    return makers.length === 1 ? `bought from ${accountName(makers[0], ctx)}` : "bought on the DEX";
  }
  if (t.sender === creator && t.recipient != null && !isDex(t.recipient, ctx) && ctx.label(t.recipient)?.kind !== "module") {
    return "paid to the seller";
  }
  return "DEX fee";
}

const createOrder = renderer({
  category: "dex",
  title: (msg, ctx) =>
    `${sideWord(str(msg, "order_type"))} order · ${orderText(str(msg, "market_id"), str(msg, "amount"), str(msg, "price"), ctx)}`,
  counterparty: () => ({ kind: "name", name: DEX_MODULE }),
  reasonFor: (t, msg, ctx) => orderReason(t, msg, ctx, orderSide(str(msg, "order_type"))),
  Body: function CreateOrderBody({ msg, ctx }: BodyProps) {
    const market = str(msg, "market_id");
    const coins = marketCoins(market, str(msg, "amount"), str(msg, "price"));
    const pct = createdPct(msg, outcomeOf(msg, ctx));
    return (
      <Stack gap="3">
        <Fields
          rows={[
            {
              label: "Placed by",
              value: <Account address={str(msg, "creator")} ctx={ctx} />,
            },
            {
              label: "Order",
              value: (
                <Text as="span">
                  {sideWord(str(msg, "order_type"))} {orderText(market, str(msg, "amount"), str(msg, "price"), ctx)} each
                  {coins?.quote && ` · ${coinText(coins.quote, ctx)} total`}
                </Text>
              ),
            },
          ]}
        />
        <DexOutcome msg={msg} ctx={ctx} />
        {pct != null && <FilledBar pct={pct} />}
        <DexLink marketId={market} ctx={ctx} />
      </Stack>
    );
  },
});

const cancelOrder = renderer({
  category: "dex",
  title: (msg, ctx) => `Cancel order #${shortOrderId(str(msg, "order_id") ?? "?")} on ${marketName(str(msg, "market_id"), ctx)}`,
  counterparty: () => ({ kind: "name", name: DEX_MODULE }),
  reasonFor: (t, msg, ctx) => orderReason(t, msg, ctx, orderSide(str(msg, "order_type"))),
  Body: function CancelOrderBody({ msg, ctx }: BodyProps) {
    return (
      <Stack gap="3">
        <Fields
          rows={[
            {
              label: "Cancelled by",
              value: <Account address={str(msg, "creator")} ctx={ctx} />,
            },
            {
              label: "Order",
              value: (
                <>
                  <OrderId id={str(msg, "order_id") ?? "?"} /> · {str(msg, "order_type") ?? "unknown side"}
                </>
              ),
            },
          ]}
        />
        <DexOutcome msg={msg} ctx={ctx} />
        <DexLink marketId={str(msg, "market_id")} ctx={ctx} />
      </Stack>
    );
  },
});

interface FillItem {
  price: string | null;
  amount: string | null;
}

function fillItems(msg: RenderMessage): FillItem[] {
  const list = msg.body?.orders;
  return Array.isArray(list)
    ? list.map((v) => {
        const o = obj(v);
        return {
          price: typeof o?.price === "string" ? o.price : null,
          amount: typeof o?.amount === "string" ? o.amount : null,
        };
      })
    : [];
}

/**
 * What a settled fill message did not fill: the orders it named were no
 * longer on the book at their price when the block ended, so the order book
 * sent their coins back. Since chain v8.3.0 the refunds say what went back;
 * before, it is inferred from the fills. Fill messages never rest and settle
 * in the block of their transaction, so nothing comes later: a pending flag
 * here is the chain leaving a refund without an event (v8.2.0 gives the
 * message an id, its refund none), not an outcome still to come, unless the
 * order book failed to process it (v8.3.0).
 */
function NotFilledLine({ msg, ctx }: BodyProps) {
  if (!ctx.dexSettled) return null;
  const outcome = outcomeOf(msg, ctx);
  if (outcome && isStuck(outcome)) return null;
  const market = str(msg, "market_id");
  const filled = new BigNumber(fillTotals(outcome?.fills ?? []).base);
  const refunds = (outcome?.refunds ?? []).filter((r) => r.reason !== "below_min_amount");
  let text: string;
  if (refunds.length > 0) {
    const back = sentBackText(refunds, ctx);
    text = filled.gt(0)
      ? `${baseText(market, refundTotals(refunds).base, ctx)} not filled: ${back}`
      : `Nothing was filled (no order at that price): ${back}`;
  } else {
    const asked = fillItems(msg).reduce((sum, f) => sum.plus(f.amount ?? 0), new BigNumber(0));
    const rest = asked.minus(filled);
    if (!rest.gt(0)) return null;
    text = filled.gt(0)
      ? `${baseText(market, rest.toFixed(), ctx)} not filled: those orders were no longer on the book at their price, so their coins were sent back`
      : "Nothing was filled: these orders were no longer on the book at their price when the block ended, so the coins were sent back";
  }
  return (
    <Text fontSize="sm" color="orange.fg" role="status" data-testid="dex-not-filled">
      {text}.
    </Text>
  );
}

const fillOrders = renderer({
  category: "dex",
  title: (msg, ctx) => {
    const n = fillItems(msg).length;
    return `Fill ${n} order${n === 1 ? "" : "s"} on ${marketName(str(msg, "market_id"), ctx)}`;
  },
  counterparty: () => ({ kind: "name", name: DEX_MODULE }),
  // Filling sell orders buys.
  reasonFor: (t, msg, ctx) => orderReason(t, msg, ctx, orderSide(str(msg, "order_type")) === "sell" ? "buy" : "sell"),
  Body: function FillOrdersBody({ msg, ctx }: BodyProps) {
    const market = str(msg, "market_id");
    return (
      <Stack gap="3">
        <Fields
          rows={[
            {
              label: "Sent by",
              value: <Account address={str(msg, "creator")} ctx={ctx} />,
            },
            {
              label: `${str(msg, "order_type") === "sell" ? "Sell" : "Buy"} orders`,
              value: (
                <Stack gap="0.5">
                  {fillItems(msg).map((f, i) => (
                    <Text key={i}>{orderText(market, f.amount, f.price, ctx)}</Text>
                  ))}
                </Stack>
              ),
            },
          ]}
        />
        <DexOutcome msg={msg} ctx={ctx} final />
        <NotFilledLine msg={msg} ctx={ctx} />
        <DexLink marketId={market} ctx={ctx} />
      </Stack>
    );
  },
});

/** What the swap paid out: the transfers of its output denom to the creator. */
function swapReceived(msg: RenderMessage, ctx: RenderContext): Coin | null {
  const out = coin(msg, "min_output");
  const creator = str(msg, "creator");
  if (!out) return null;
  const got = transfersOf(msg, ctx).filter((t) => t.recipient === creator && t.denom === out.denom);
  if (got.length === 0) return null;
  return {
    denom: out.denom,
    amount: got.reduce((s, t) => s.plus(t.amount), new BigNumber(0)).toFixed(),
  };
}

function routes(msg: RenderMessage): string[] {
  const r = msg.body?.routes;
  return Array.isArray(r) ? r.filter((x): x is string => typeof x === "string") : [];
}

/**
 * A pool id `base_quote` (sorted denoms joined by `_`) split into its denoms:
 * the split whose sides are both known wins, else the first `_`.
 */
function poolDenoms(poolId: string | null, ctx: RenderContext): [string, string] | null {
  if (!poolId) return null;
  for (let i = poolId.indexOf("_"); i > 0; i = poolId.indexOf("_", i + 1)) {
    const pair: [string, string] = [poolId.slice(0, i), poolId.slice(i + 1)];
    if (ctx.denom(pair[0]).symbol != null && ctx.denom(pair[1]).symbol != null) return pair;
  }
  const i = poolId.indexOf("_");
  return i > 0 ? [poolId.slice(0, i), poolId.slice(i + 1)] : null;
}

/** `BZE/USDC`; the raw pool id when a denom is unknown. */
function poolName(poolId: string | null, ctx: RenderContext): string {
  const d = poolDenoms(poolId, ctx);
  const a = d && ctx.denom(d[0]).symbol;
  const b = d && ctx.denom(d[1]).symbol;
  return a != null && b != null ? `${a}/${b}` : (poolId ?? "an unknown");
}

const multiSwap = renderer({
  category: "dex",
  title: (msg, ctx) => {
    const input = coin(msg, "input");
    const out = coin(msg, "min_output");
    return `Swap ${input ? coinText(input, ctx) : "tokens"} → ${out ? (ctx.denom(out.denom).symbol ?? out.denom) : "another token"}`;
  },
  counterparty: () => ({ kind: "name", name: DEX_MODULE }),
  reasonFor: (t, msg) => (t.sender === str(msg, "creator") ? "swapped" : "received from the swap"),
  Body: function MultiSwapBody({ msg, ctx }: BodyProps) {
    const input = coin(msg, "input");
    const min = coin(msg, "min_output");
    const got = swapReceived(msg, ctx);
    return (
      <Fields
        rows={[
          {
            label: "Swapped by",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          { label: "Paid", value: input && <Coins list={[input]} ctx={ctx} /> },
          { label: "Received", value: got && <Coins list={[got]} ctx={ctx} /> },
          { label: "At least", value: min && coinText(min, ctx) },
          {
            label: "Through",
            value: routes(msg).length
              ? routes(msg)
                  .map((r) => `${poolName(r, ctx)} pool`)
                  .join(" → ")
              : null,
          },
        ]}
      />
    );
  },
});

/** The two amounts of a pool: `100 BZE + 1.25 USDC`. */
function pairText(poolId: string | null, a: string | null, b: string | null, ctx: RenderContext): string | null {
  const denoms = poolDenoms(poolId, ctx);
  if (!denoms || a == null || b == null) return null;
  return `${coinText({ denom: denoms[0], amount: a }, ctx)} + ${coinText({ denom: denoms[1], amount: b }, ctx)}`;
}

const addLiquidity = renderer({
  category: "dex",
  title: (msg, ctx) => {
    const pair = pairText(str(msg, "pool_id"), str(msg, "base_amount"), str(msg, "quote_amount"), ctx);
    return `Add liquidity to the ${poolName(str(msg, "pool_id"), ctx)} pool${pair ? ` · ${pair}` : ""}`;
  },
  counterparty: () => ({ kind: "name", name: DEX_MODULE }),
  reasonFor: (t, msg) => (t.sender === str(msg, "creator") ? "added to the pool" : "pool shares received"),
  Body: function AddLiquidityBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          {
            label: "Added by",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          {
            label: "Amounts",
            value: pairText(str(msg, "pool_id"), str(msg, "base_amount"), str(msg, "quote_amount"), ctx),
          },
          {
            label: "Pool",
            value: (
              <Text as="span" fontFamily="mono" wordBreak="break-all">
                {str(msg, "pool_id")}
              </Text>
            ),
          },
        ]}
      />
    );
  },
});

const removeLiquidity = renderer({
  category: "dex",
  title: (msg, ctx) => `Remove liquidity from the ${poolName(str(msg, "pool_id"), ctx)} pool`,
  counterparty: () => ({ kind: "name", name: DEX_MODULE }),
  reasonFor: (t, msg) => (t.sender === str(msg, "creator") ? "pool shares returned" : "withdrawn from the pool"),
  Body: function RemoveLiquidityBody({ msg, ctx }: BodyProps) {
    const back = transfersOf(msg, ctx).filter((t) => t.recipient === str(msg, "creator"));
    return (
      <Fields
        rows={[
          {
            label: "Removed by",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          {
            label: "Received",
            value: back.length ? <Coins list={back.map((t) => ({ denom: t.denom, amount: t.amount }))} ctx={ctx} /> : null,
          },
          {
            label: "At least",
            value: pairText(str(msg, "pool_id"), str(msg, "min_base"), str(msg, "min_quote"), ctx),
          },
          {
            label: "Pool",
            value: (
              <Text as="span" fontFamily="mono" wordBreak="break-all">
                {str(msg, "pool_id")}
              </Text>
            ),
          },
        ]}
      />
    );
  },
});

/** `BZE/USDC` from a body's `base` and `quote`. */
function pairName(msg: RenderMessage, ctx: RenderContext): string {
  const base = str(msg, "base");
  const quote = str(msg, "quote");
  if (!base || !quote) return "a new";
  return marketName(`${base}/${quote}`, ctx);
}

const createPool = renderer({
  category: "dex",
  title: (msg, ctx) => `Create the ${pairName(msg, ctx)} pool`,
  counterparty: () => ({ kind: "name", name: DEX_MODULE }),
  reasonFor: (t, msg) => (t.sender === str(msg, "creator") ? "added to the new pool" : "pool shares received"),
  Body: function CreatePoolBody({ msg, ctx }: BodyProps) {
    const base = str(msg, "base");
    const quote = str(msg, "quote");
    const fee = str(msg, "fee");
    const pct = fee != null ? new BigNumber(fee).times(100) : null;
    return (
      <Fields
        rows={[
          {
            label: "Created by",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          {
            label: "First liquidity",
            value:
              base && quote && str(msg, "initial_base") && str(msg, "initial_quote")
                ? `${coinText({ denom: base, amount: str(msg, "initial_base")! }, ctx)} + ${coinText({ denom: quote, amount: str(msg, "initial_quote")! }, ctx)}`
                : null,
          },
          {
            label: "Swap fee",
            value: pct?.isFinite() ? `${pct.toFormat()}%` : null,
          },
          {
            label: "Kind",
            value: msg.body?.stable === true ? "stable pair" : null,
          },
        ]}
      />
    );
  },
});

const createMarket = renderer({
  category: "dex",
  title: (msg, ctx) => `Open the ${pairName(msg, ctx)} market`,
  counterparty: () => ({ kind: "name", name: DEX_MODULE }),
  reasonFor: () => "market creation fee",
  Body: function CreateMarketBody({ msg, ctx }: BodyProps) {
    const base = str(msg, "base");
    const quote = str(msg, "quote");
    return (
      <Stack gap="3">
        <Fields
          rows={[
            {
              label: "Opened by",
              value: <Account address={str(msg, "creator")} ctx={ctx} />,
            },
            { label: "Base", value: base },
            { label: "Quote", value: quote },
          ]}
        />
        {base && quote && <DexLink marketId={`${base}/${quote}`} ctx={ctx} />}
      </Stack>
    );
  },
});

const T = "/bze.tradebin.";

export const tradebinRenderers: Record<string, MessageRenderer> = {
  [`${T}MsgCreateOrder`]: createOrder,
  [`${T}MsgCancelOrder`]: cancelOrder,
  [`${T}MsgFillOrders`]: fillOrders,
  [`${T}MsgMultiSwap`]: multiSwap,
  [`${T}MsgAddLiquidity`]: addLiquidity,
  [`${T}MsgRemoveLiquidity`]: removeLiquidity,
  [`${T}MsgCreateLiquidityPool`]: createPool,
  [`${T}MsgCreateMarket`]: createMarket,
};
