import { readFileSync } from "node:fs";
import path from "node:path";

import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Fill, IbcTransfer, Order, Refund, Transfer, TxDex } from "@/api/types";
import { fakeApi, renderWithProviders } from "@/test/render";

import { dexMarketUrl, marketName, outcomeOf, priceText, splitMarket } from "./dex";
import { fallbackRenderer } from "./fallback";
import { createRenderContext, registry, rendererFor } from "./registry";
import type { RenderContext, RenderMessage } from "./types";

// vitest runs from ui/.
const DIR = path.resolve("test/fixtures/messages") + path.sep;
const CLASSIFY = path.resolve("../backend/internal/classify/classify.go");

function fixture(name: string): RenderMessage {
  return { msg_index: 0, ...JSON.parse(readFileSync(`${DIR}${name}.json`, "utf8")) };
}

const A = "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw";
const B = "bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4";
const MAKER = "bze1q8f7z6x5c4v3d2n0mlkjhgfdsapzxu4a0ex0rq";
const DEX = "bze18mhtjwczlzqqgvw84uz8lrdv4hqule3jp8allp";
const BURNER = "bze1pj73qt0lk9q64vwx4veu53ez6la2409wewcfqq";
const USDC = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4";
const ATOM = "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2";
const MARKET = `ubze/${USDC}`;

const tokens = new Map([
  ["ubze", { symbol: "BZE", exponent: 6, price_usd: null }],
  [USDC, { symbol: "USDC", exponent: 6, price_usd: null }],
  [ATOM, { symbol: "ATOM", exponent: 6, price_usd: null }],
]);

function transfer(seq: number, sender: string, recipient: string, denom: string, amount: string): Transfer {
  const label = (a: string) => (a === DEX ? { name: "DEX", kind: "module" } : a === BURNER ? { name: "Burner", kind: "module" } : null);
  return { seq, msg_index: 0, kind: "transfer", sender, sender_label: label(sender), recipient, recipient_label: label(recipient), denom, amount, symbol: null, exponent: null };
}

function order(o: Partial<Order>): Order {
  return {
    order_id: "1042",
    market_id: MARKET,
    owner: A,
    side: "buy",
    price: "0.0125",
    amount: "400000000",
    filled_amount: "0",
    status: "open",
    created_height: 25120440,
    created_time: "2026-10-11T09:15:02Z",
    created_tx_hash: "AB".repeat(32),
    message_id: "000000000000000000000003",
    closed_height: null,
    closed_time: null,
    ...o,
  };
}

function fill(f: Partial<Fill>): Fill {
  return {
    height: 25120440,
    time: "2026-10-11T09:15:02Z",
    order_id: "998",
    market_id: MARKET,
    side: "sell",
    price: "0.0125",
    amount: "600000000",
    maker: MAKER,
    taker: A,
    taker_tx_hash: "AB".repeat(32),
    taker_message_id: "000000000000000000000003",
    ...f,
  };
}

const dexCtx = (dex: TxDex, transfers: Transfer[] = []) => createRenderContext({ transfers, tokens, dex });

/** A settled transaction's context: nothing placed, filled or cancelled unless given. */
const settledDex = (dex: Partial<TxDex>) =>
  createRenderContext({ transfers: [], tokens, dex: { placed: [], fills: [], cancelled: [], refunds: [], pending: false, ...dex }, dexSettled: true });

function body(msg: RenderMessage, ctx: RenderContext) {
  const r = rendererFor(msg.type_url);
  return renderWithProviders(<r.Body msg={msg} ctx={ctx} />);
}

describe("BZE renderers", () => {
  it("covers every BZE message type the backend classifies, without the fallback", () => {
    const types = [...readFileSync(CLASSIFY, "utf8").matchAll(/msg\("(\/bze\.[^"]+)"/g)].map((m) => m[1]);
    expect(types.length).toBeGreaterThan(30);
    expect(types.filter((t) => !registry.has(t) || rendererFor(t) === fallbackRenderer)).toEqual([]);
  });
});

describe("markets", () => {
  it("splits a market id whose denoms hold slashes", () => {
    expect(splitMarket(MARKET)).toEqual({ base: "ubze", quote: USDC });
    expect(splitMarket(`${USDC}/ubze`)).toEqual({ base: USDC, quote: "ubze" });
    expect(splitMarket(`factory/${A}/uhoney/ubze`)).toEqual({ base: `factory/${A}/uhoney`, quote: "ubze" });
    expect(splitMarket("ubze")).toBeNull();
    expect(splitMarket("ubze/ibc")).toBeNull();
    expect(splitMarket("ibc/ubze")).toBeNull();
    expect(splitMarket("ubze//uusdc")).toBeNull();
  });

  it("names a market by its symbols, by its raw id when a denom is unknown", () => {
    const ctx = createRenderContext({ transfers: [], tokens });
    expect(marketName(MARKET, ctx)).toBe("BZE/USDC");
    expect(marketName("ubze/uhoney", ctx)).toBe("ubze/uhoney");
  });

  it("shows a price in display units, shifted by the exponents", () => {
    const ctx = createRenderContext({ transfers: [], tokens: new Map([...tokens, ["usat", { symbol: "SAT", exponent: 8, price_usd: null }]]) });
    expect(priceText("0.0125", MARKET, ctx)).toBe("0.0125 USDC");
    // 223 ubze per base unit of an 8-decimal token: 223 × 10⁸ ubze, 22,300 BZE per SAT.
    expect(priceText("223", "usat/ubze", ctx)).toBe("22,300 BZE");
    expect(priceText("223", "uhoney/ubze", ctx)).toBe("223 ubze");
  });

  it("links to the market on the DEX app", () => {
    expect(dexMarketUrl(MARKET)).toBe(`https://dex.getbze.com/exchange/market?id=ubze%2F${encodeURIComponent(USDC)}`);
  });
});

describe("DEX outcome", () => {
  const create = fixture("tradebin-create-order");

  it("shows the fills, the remainder on the book and how much filled", () => {
    const ctx = dexCtx({ placed: [order({})], fills: [fill({})], cancelled: [], pending: false });
    expect(outcomeOf(create, ctx)?.fills).toHaveLength(1);
    body(create, ctx);
    expect(screen.getByTestId("dex-fills")).toHaveTextContent("Filled right away: 600 BZE for 7.5 USDC");
    expect(within(screen.getByTestId("dex-fills")).getByRole("link", { name: "bze1q8f…x0rq" })).toBeInTheDocument();
    expect(screen.getByTestId("dex-placed")).toHaveTextContent("400 BZE still waiting on the order book · order #1042");
    expect(screen.getByRole("link", { name: "order #1042" })).toHaveAttribute("href", `/accounts/${A}?tab=activity`);
    expect(screen.getByTestId("dex-filled")).toHaveTextContent("60% filled");
    expect(screen.getByText(/Buy 1,000 BZE at 0.0125 USDC each · 12.5 USDC total/)).toBeInTheDocument();
    expect(screen.getByText(/Open BZE\/USDC on/)).toHaveTextContent("Open BZE/USDC on DEX");
    expect(screen.getByRole("link", { name: "DEX" })).toHaveAttribute("href", dexMarketUrl(MARKET));
    expect(screen.queryByTestId("dex-pending")).not.toBeInTheDocument();
  });

  it("shows a fully filled order at 100%", () => {
    const ctx = dexCtx({ placed: [], fills: [fill({ amount: "400000000" }), fill({ amount: "600000000", maker: B })], cancelled: [], pending: false });
    body(create, ctx);
    expect(screen.getByTestId("dex-fills")).toHaveTextContent("Filled right away: 1,000 BZE for 12.5 USDC");
    expect(screen.queryByTestId("dex-placed")).not.toBeInTheDocument();
    expect(screen.getByTestId("dex-filled")).toHaveTextContent("100% filled");
  });

  it("says what a cancel returned", () => {
    const cancel = fixture("tradebin-cancel-order");
    const ctx = dexCtx({ placed: [], fills: [], cancelled: [order({ status: "cancelled" })], pending: false });
    body(cancel, ctx);
    expect(screen.getByTestId("dex-cancelled")).toHaveTextContent("Order #1042 cancelled, 5 USDC returned");
  });

  it("trims a long order id to its first character and last seven, the full id on hover", () => {
    const short = fixture("tradebin-cancel-order");
    body(short, dexCtx({ placed: [], fills: [], cancelled: [order({ status: "cancelled" })], pending: false }));
    expect(screen.getAllByTestId("order-id").map((e) => [e.textContent, e.getAttribute("title")])).toEqual([
      ["#1042", "1042"],
      ["#1042", "1042"],
    ]);

    const id = "000000000000000006714623";
    const long = { ...short, body: { ...short.body, order_id: id } };
    const ctx = dexCtx({ placed: [], fills: [], cancelled: [order({ status: "cancelled", order_id: id })], pending: false });
    expect(rendererFor(long.type_url).title(long, ctx)).toBe("Cancel order #0…6714623 on BZE/USDC");
    body(long, ctx);
    const ids = screen.getAllByTestId("order-id").slice(-2);
    expect(ids.map((e) => [e.textContent, e.getAttribute("title")])).toEqual([
      ["#0…6714623", id],
      ["#0…6714623", id],
    ]);
    expect(screen.getAllByTestId("dex-cancelled").at(-1)).toHaveTextContent("Order #0…6714623 cancelled");
  });

  it("says pending, never failed, until the order book processes the message", () => {
    const ctx = dexCtx({ placed: [], fills: [], cancelled: [], pending: true });
    body(create, ctx);
    expect(screen.getByTestId("dex-pending")).toHaveTextContent("The order book processes this at the end of the block");
    expect(screen.queryByTestId("dex-filled")).not.toBeInTheDocument();
    expect(screen.queryByText(/fail/i)).not.toBeInTheDocument();
  });

  it("keeps another message's outcome out, by queue message id or, before ids, by market and owner", () => {
    const other = fill({ taker_message_id: "000000000000000000000009", maker: B });
    expect(outcomeOf(create, dexCtx({ placed: [], fills: [fill({}), other], cancelled: [], pending: false }))?.fills).toHaveLength(1);

    const noIds = { ...create, events: [] };
    const old = dexCtx({ placed: [], fills: [fill({ taker_message_id: null }), fill({ taker_message_id: null, taker: B })], cancelled: [], pending: false });
    expect(outcomeOf(noIds, old)?.fills.map((f) => f.taker)).toEqual([A]);
  });

  describe("chain v8.3.0 order book, in the message's own events", () => {
    const TAKER = "000000000000000006714610";
    const OTHER_TAKER = "000000000000000006714611";
    const event = (type: string, attrs: Record<string, unknown>) => ({ type: `bze.tradebin.${type}`, attrs });
    const ownFill = (extra: Partial<Fill> = {}) => fill({ taker_message_id: null, taker_order_id: TAKER, ...extra });
    const msgOf = (base: RenderMessage, events: RenderMessage["events"]): RenderMessage => ({ ...base, events });
    const mk = (over: Partial<Order> = {}) => order({ order_id: TAKER, message_id: null, ...over });

    it("matches the placed order and the fills by order id, not by market and owner", () => {
      const msg = msgOf(create, [
        event("OrderExecutedEvent", { id: "998", order_id: TAKER }),
        event("OrderSavedEvent", { id: TAKER, order_id: TAKER }),
      ]);
      // A second create of the same owner and market in the transaction: its own ids.
      const dex: TxDex = {
        placed: [mk(), mk({ order_id: OTHER_TAKER })],
        fills: [ownFill(), ownFill({ taker_order_id: OTHER_TAKER, order_id: "999" }), ownFill({ taker_order_id: null })],
        cancelled: [],
        pending: false,
      };
      const out = outcomeOf(msg, dexCtx(dex));
      expect(out?.placed.map((o) => o.order_id)).toEqual([TAKER]);
      expect(out?.fills.map((f) => f.order_id)).toEqual(["998"]);
      expect(out?.pending).toBe(false);
    });

    it("shows its fills and the remainder on the book", () => {
      const msg = msgOf(create, [event("OrderExecutedEvent", { id: "998", order_id: TAKER }), event("OrderSavedEvent", { id: TAKER })]);
      body(msg, dexCtx({ placed: [mk()], fills: [ownFill()], cancelled: [], pending: false }));
      expect(screen.getByTestId("dex-fills")).toHaveTextContent("Filled right away: 600 BZE for 7.5 USDC");
      expect(screen.getByTestId("dex-placed")).toHaveTextContent("400 BZE still waiting on the order book");
      expect(screen.queryByTestId("dex-pending")).not.toBeInTheDocument();
    });

    it("a message filled in full has fills only", () => {
      const msg = msgOf(create, [event("OrderExecutedEvent", { id: "998", order_id: TAKER })]);
      const out = outcomeOf(msg, dexCtx({ placed: [], fills: [ownFill({ amount: "1000000000" })], cancelled: [], pending: false }));
      expect(out?.placed).toEqual([]);
      expect(out?.fills).toHaveLength(1);
    });

    it("matches a cancel by the order its own event names", () => {
      const cancel = fixture("tradebin-cancel-order");
      const msg = msgOf(cancel, [event("OrderCanceledEvent", { id: "1042", order_id: "1042" })]);
      const out = outcomeOf(
        msg,
        dexCtx({ placed: [], fills: [], cancelled: [order({ order_id: "1042", status: "cancelled" }), order({ order_id: "7", status: "cancelled" })], pending: false }),
      );
      expect(out?.cancelled.map((o) => o.order_id)).toEqual(["1042"]);
      body(msg, dexCtx({ placed: [], fills: [], cancelled: [order({ order_id: "1042", status: "cancelled" })], pending: false }));
      expect(screen.getByTestId("dex-cancelled")).toHaveTextContent("Order #1042 cancelled, 5 USDC returned");
    });

    it("reads a v8.2.x message with only its queue events, or a refund event, as before", () => {
      const queued = msgOf(create, [event("OrderCreateMessageEvent", { message_id: "000000000000000000000003" })]);
      const dex = dexCtx({ placed: [order({})], fills: [fill({}), ownFill()], cancelled: [], pending: true });
      const out = outcomeOf(queued, dex);
      expect(out?.placed).toHaveLength(1);
      expect(out?.fills).toHaveLength(1);
      expect(out?.pending).toBe(true);
      // Not an outcome event of this chain version: the old matching still applies.
      const refunded = msgOf(create, [event("OrderMessageRefundedEvent", { message_id: "000000000000000000000003" })]);
      expect(outcomeOf(refunded, dex)?.pending).toBe(true);
    });

    it("keeps the old paths for a message without these events", () => {
      const out = outcomeOf(create, dexCtx({ placed: [order({})], fills: [fill({}), ownFill()], cancelled: [], pending: true }));
      expect(out?.fills).toHaveLength(1);
      expect(out?.pending).toBe(true);
    });

    describe("merged orders and refunds in the transaction", () => {
      const PREVIOUS = "000000000000000006714500";
      const amended = (extra: Record<string, unknown> = {}) =>
        event("OrderAmendedEvent", { id: TAKER, previous_id: PREVIOUS, added_amount: "400000000", amount: "1000000000", order_id: TAKER, ...extra });
      const merged = (over: Partial<Order> = {}) => mk({ merged_from: PREVIOUS, added_amount: "400000000", amount: "1000000000", ...over });
      const refundEvent = (orderId: string) => event("OrderMessageRefundedEvent", { order_id: orderId, reason: "max_fills_reached" });
      const own = (r: Partial<Refund>): Refund => ({
        height: 25191401,
        order_id: TAKER,
        market_id: MARKET,
        order_type: "buy",
        amount: "9000000000",
        refund: { denom: USDC, amount: "112500000" },
        reason: "max_fills_reached",
        ...r,
      });

      it("says the create was added to the order the owner already rested at this price", () => {
        const msg = msgOf(create, [amended()]);
        const out = outcomeOf(msg, dexCtx({ placed: [merged()], fills: [], cancelled: [], pending: false }));
        expect(out?.placed.map((o) => o.order_id)).toEqual([TAKER]);
        body(msg, dexCtx({ placed: [merged()], fills: [], cancelled: [], pending: false }));
        expect(screen.getByTestId("dex-placed")).toHaveTextContent(
          "Added 400 BZE to your order at this price; it now rests as order #0…6714610 (total 1,000 BZE), at the back of the queue",
        );
        expect(screen.getByRole("link", { name: "order #0…6714610" })).toBeInTheDocument();
        expect(screen.queryByTestId("dex-pending")).not.toBeInTheDocument();
      });

      it("trims the merged order's id like any other", () => {
        const id = "000000000000000006714623";
        body(msgOf(create, [amended({ id, order_id: id })]), dexCtx({ placed: [merged({ order_id: id })], fills: [], cancelled: [], pending: false }));
        expect(screen.getByTestId("dex-placed")).toHaveTextContent("it now rests as order #0…6714623 (total 1,000 BZE)");
        expect(screen.getByTestId("order-id")).toHaveAttribute("title", id);
      });

      it("shows the fills and the merge of one message together", () => {
        const msg = msgOf(create, [event("OrderExecutedEvent", { id: "998", order_id: TAKER }), amended()]);
        body(msg, dexCtx({ placed: [merged({ added_amount: "400000000" })], fills: [ownFill()], cancelled: [], pending: false }));
        expect(screen.getByTestId("dex-fills")).toHaveTextContent("Filled right away: 600 BZE for 7.5 USDC");
        expect(screen.getByTestId("dex-placed")).toHaveTextContent("Added 400 BZE to your order at this price");
        expect(screen.getByTestId("dex-filled")).toHaveTextContent("60% filled");
      });

      it("says what became of a merged order since", () => {
        const msg = msgOf(create, [amended()]);
        const text = (status: Order["status"]) => {
          const view = body(msg, dexCtx({ placed: [merged({ status })], fills: [], cancelled: [], pending: false }));
          const out = screen.getByTestId("dex-placed").textContent;
          view.unmount();
          return out;
        };
        expect(text("open")).toMatch(/at the back of the queue$/);
        expect(text("merged")).toMatch(/at the back of the queue; since merged into a later order$/);
        expect(text("filled")).toMatch(/at the back of the queue; since filled$/);
        expect(text("cancelled")).toMatch(/at the back of the queue; since cancelled$/);
      });

      it("words a plain order that was merged into a later one", () => {
        const msg = msgOf(create, [event("OrderSavedEvent", { id: TAKER, order_id: TAKER })]);
        body(msg, dexCtx({ placed: [mk({ status: "merged", filled_amount: "100000000" })], fills: [], cancelled: [], pending: false }));
        expect(screen.getByTestId("dex-placed")).toHaveTextContent("300 BZE rested on the order book, since merged into a later order");
      });

      it("keeps another message's merge out", () => {
        const msg = msgOf(create, [amended()]);
        const out = outcomeOf(msg, dexCtx({ placed: [merged(), merged({ order_id: OTHER_TAKER })], fills: [], cancelled: [], pending: false }));
        expect(out?.placed.map((o) => o.order_id)).toEqual([TAKER]);
      });

      it("reads a refund event with an order id as the v8.3.0 outcome, not the v8.2.x queue", () => {
        const msg = msgOf(create, [refundEvent(TAKER)]);
        const dex = dexCtx({ placed: [mk()], fills: [], cancelled: [], refunds: [own({}), own({ order_id: OTHER_TAKER })], pending: true });
        const out = outcomeOf(msg, dex);
        expect(out?.pending).toBe(false);
        expect(out?.placed).toEqual([]);
        expect(out?.refunds.map((r) => r.order_id)).toEqual([TAKER]);
      });

      it("says a create stopped at the fill cap, with the cap from the parameters", async () => {
        const msg = msgOf(create, [event("OrderExecutedEvent", { id: "998", order_id: TAKER }), refundEvent(TAKER)]);
        const api = fakeApi({ params: async () => ({ tradebin: { params: { maxFillsPerOrder: "500" }, last_change: null } }) });
        const dex = settledDex({ fills: [ownFill()], refunds: [own({})] });
        const r = rendererFor(msg.type_url);
        renderWithProviders(<r.Body msg={msg} ctx={dex} />, { api });
        expect(screen.getByTestId("dex-max-fills")).toBeInTheDocument();
        expect(await screen.findByText(/Stopped after the most fills one order can take \(500\);/)).toHaveTextContent(
          "Stopped after the most fills one order can take (500); 9,000 BZE was not filled and 112.5 USDC was sent back. Send another order for the rest.",
        );
        expect(screen.queryByTestId("dex-placed")).not.toBeInTheDocument();
        expect(screen.queryByTestId("dex-below-min")).not.toBeInTheDocument();
      });

      it("leaves the cap out while the parameters are unknown, and says dust was kept", () => {
        const msg = msgOf(create, [refundEvent(TAKER)]);
        body(msg, settledDex({ refunds: [own({ amount: "1", refund: null })] }));
        expect(screen.getByTestId("dex-max-fills")).toHaveTextContent(
          "Stopped after the most fills one order can take; 0.000001 BZE was not filled (too small to send back, kept as dust). Send another order for the rest.",
        );
      });

      it("says one fill entry stopped at the cap without the old no-orders wording", () => {
        const fillMsg = fixture("tradebin-fill-orders");
        const msg = msgOf(fillMsg, [event("OrderExecutedEvent", { id: "998", order_id: TAKER }), refundEvent(TAKER), refundEvent(OTHER_TAKER)]);
        const refunds = [own({}), own({ order_id: OTHER_TAKER, amount: "1000000000", refund: { denom: USDC, amount: "12500000" } })];
        body(msg, settledDex({ fills: [ownFill({ amount: "200000000" })], refunds }));
        expect(screen.getByTestId("dex-max-fills")).toHaveTextContent("10,000 BZE was not filled and 125 USDC was sent back.");
        expect(screen.queryByTestId("dex-not-filled")).not.toBeInTheDocument();
      });

      it("keeps the old not-filled wording for the older reasons of a v8.3.0 fill message", () => {
        const fillMsg = fixture("tradebin-fill-orders");
        const msg = msgOf(fillMsg, [refundEvent(TAKER)]);
        body(msg, settledDex({ refunds: [own({ reason: "no_match", amount: "200000000", refund: { denom: "ubze", amount: "200000000" } })] }));
        expect(screen.getByTestId("dex-not-filled")).toHaveTextContent("Nothing was filled (no order at that price): 200 BZE sent back.");
        expect(screen.queryByTestId("dex-max-fills")).not.toBeInTheDocument();
      });
    });
  });

  it("says a settled fill filled nothing, never that its orders were filled", () => {
    // Mainnet tx 775A6F91…: both orders were gone, v8.1.1 refunded the coins and emitted no DEX event (dex null).
    const fillMsg = fixture("tradebin-fill-orders");
    const ctx = createRenderContext({ transfers: [], tokens, dex: null, dexSettled: true });
    body(fillMsg, ctx);
    expect(screen.getByTestId("dex-not-filled")).toHaveTextContent(
      "Nothing was filled: these orders were no longer on the book at their price when the block ended, so the coins were sent back.",
    );
    expect(screen.queryByText(/filled by/i)).not.toBeInTheDocument();
    expect(screen.getByText("Sent by")).toBeInTheDocument();
  });

  it("says how much of a settled fill was not filled", () => {
    const fillMsg = fixture("tradebin-fill-orders");
    const creator = String(fillMsg.body?.creator);
    const ctx = createRenderContext({
      transfers: [],
      tokens,
      dex: { placed: [], fills: [fill({ taker: creator, taker_message_id: null, amount: "200000000", price: "0.0124" })], cancelled: [], pending: false },
      dexSettled: true,
    });
    body(fillMsg, ctx);
    expect(screen.getByTestId("dex-fills")).toHaveTextContent("Filled right away: 200 BZE");
    expect(screen.getByTestId("dex-not-filled")).toHaveTextContent("300 BZE not filled");
  });

  it("claims nothing about a fill's outcome in a list or for a failed transaction", () => {
    const fillMsg = fixture("tradebin-fill-orders");
    body(fillMsg, createRenderContext({ transfers: [], tokens }));
    body(fillMsg, createRenderContext({ transfers: [], tokens, dex: null, dexSettled: false }));
    expect(screen.queryByTestId("dex-not-filled")).not.toBeInTheDocument();
  });

  it("never leaves a settled fill pending: v8.2.0 refunds a fill with an id but no event", () => {
    const fillMsg = { ...fixture("tradebin-fill-orders"), events: [{ type: "bze.tradebin.OrderCreateMessageEvent", attrs: { message_id: "000000000000000000000007" } }] };
    body(fillMsg, createRenderContext({ transfers: [], tokens, dex: { placed: [], fills: [], cancelled: [], pending: true }, dexSettled: true }));
    expect(screen.queryByTestId("dex-pending")).not.toBeInTheDocument();
    expect(screen.getByTestId("dex-not-filled")).toHaveTextContent("Nothing was filled");
  });

  describe("chain v8.3.0 outcomes by queue message id (the upgrade block and pre-release builds)", () => {
    const id = (n: number) => String(n).padStart(24, "0");
    const v830Fill = {
      ...fixture("tradebin-fill-orders"),
      events: [1, 2].map((n) => ({ type: "bze.tradebin.OrderCreateMessageEvent", attrs: { message_id: id(n) } })),
    };
    const refund = (r: Partial<Refund>): Refund => ({
      height: 25191401,
      message_id: id(1),
      market_id: MARKET,
      order_type: "sell",
      amount: "200000000",
      refund: { denom: "ubze", amount: "200000000" },
      reason: "no_match",
      ...r,
    });
    const settled = (dex: Partial<TxDex>) =>
      createRenderContext({
        transfers: [],
        tokens,
        dex: { placed: [], fills: [], cancelled: [], refunds: [], pending: false, ...dex },
        dexSettled: true,
      });

    it("says what a fill that found nothing got back", () => {
      body(v830Fill, settled({ refunds: [refund({}), refund({ message_id: id(2), amount: "300000000", refund: { denom: "ubze", amount: "300000000" } })] }));
      expect(screen.getByTestId("dex-not-filled")).toHaveTextContent("Nothing was filled (no order at that price): 500 BZE sent back.");
    });

    it("says how much of a partly matched fill went back", () => {
      const taken = fill({ taker: A, taker_message_id: id(1), amount: "200000000", price: "0.0124" });
      body(v830Fill, settled({ fills: [taken], refunds: [refund({ message_id: id(2), amount: "300000000", refund: { denom: "ubze", amount: "300000000" }, reason: "unfilled_remainder" })] }));
      expect(screen.getByTestId("dex-fills")).toHaveTextContent("Filled right away: 200 BZE");
      expect(screen.getByTestId("dex-not-filled")).toHaveTextContent("300 BZE not filled: 300 BZE sent back.");
    });

    it("says a refund too small to send back was kept as dust", () => {
      body(v830Fill, settled({ refunds: [refund({ amount: "1", refund: null })] }));
      expect(screen.getByTestId("dex-not-filled")).toHaveTextContent("Nothing was filled (no order at that price): too small to send back, kept as dust.");
    });

    it("ignores another message's refund", () => {
      body(v830Fill, settled({ refunds: [refund({ message_id: id(9) })] }));
      // No refund of its own: the PR #121 inference.
      expect(screen.getByTestId("dex-not-filled")).toHaveTextContent("Nothing was filled: these orders were no longer on the book");
    });

    it("says a queued create the upgrade sent back, on its original transaction", () => {
      const queued = { ...create, events: [{ type: "bze.tradebin.OrderCreateMessageEvent", attrs: { message_id: id(3) } }] };
      const r = refund({ message_id: id(3), order_id: id(3), order_type: "buy", amount: "400000000", refund: { denom: USDC, amount: "5000000" }, reason: "upgrade" });
      expect(outcomeOf(queued, settled({ refunds: [r], pending: false }))?.refunds).toEqual([r]);
      body(queued, settled({ refunds: [r] }));
      expect(screen.getByTestId("dex-upgrade")).toHaveTextContent(
        "This order was still waiting when the chain upgraded to v8.3.0 and was sent back: 5 USDC. Place it again.",
      );
      expect(screen.queryByTestId("dex-pending")).not.toBeInTheDocument();
      expect(screen.queryByTestId("dex-below-min")).not.toBeInTheDocument();
    });

    it("says a queued fill the upgrade sent back, and does not call it unfilled for want of orders", () => {
      const r = refund({ message_id: id(1), order_id: id(1), reason: "upgrade" });
      body(v830Fill, settled({ refunds: [r] }));
      expect(screen.getByTestId("dex-upgrade")).toHaveTextContent("sent back: 200 BZE. Place it again.");
      expect(screen.queryByTestId("dex-not-filled")).not.toBeInTheDocument();
    });

    it("says an upgrade refund that was only dust", () => {
      const queued = { ...create, events: [{ type: "bze.tradebin.OrderCreateMessageEvent", attrs: { message_id: id(3) } }] };
      body(queued, settled({ refunds: [refund({ message_id: id(3), reason: "upgrade", amount: "1", refund: null })] }));
      expect(screen.getByTestId("dex-upgrade")).toHaveTextContent("was sent back: too small to send back, kept as dust. Place it again.");
    });

    it("says a create's remainder below the minimum went back", () => {
      const placedFill = fill({});
      body(create, settled({ fills: [placedFill], refunds: [refund({ message_id: id(3), order_type: "buy", amount: "400000000", refund: { denom: USDC, amount: "5000000" }, reason: "below_min_amount" })] }));
      expect(screen.getByTestId("dex-below-min")).toHaveTextContent("The rest was below the minimum order: 5 USDC sent back.");
      expect(screen.queryByTestId("dex-pending")).not.toBeInTheDocument();
    });
  });

  it("keeps a placed order pending until the order book processes it", () => {
    body(create, createRenderContext({ transfers: [], tokens, dex: { placed: [], fills: [], cancelled: [], pending: true }, dexSettled: true }));
    expect(screen.getByTestId("dex-pending")).toBeInTheDocument();
  });

  it("shows nothing without an outcome (a list's context)", () => {
    body(create, createRenderContext({ transfers: [], tokens }));
    expect(screen.queryByTestId("dex-outcome")).not.toBeInTheDocument();
    expect(screen.queryByTestId("dex-pending")).not.toBeInTheDocument();
  });

  it("tells the held funds from the order fee and a cancel's refund", () => {
    const held = transfer(1, A, DEX, USDC, "12500000");
    const fee = transfer(2, A, DEX, "ubze", "1000");
    const passed = transfer(3, DEX, BURNER, "ubze", "1000");
    const ctx = dexCtx({ placed: [], fills: [fill({})], cancelled: [], pending: false }, [held, fee, passed]);
    const r = rendererFor(create.type_url);
    expect([held, fee, passed].map((t) => r.reasonFor!(t, create, ctx))).toEqual(["held for the buy order", "DEX fee", "DEX fee"]);

    const cancel = fixture("tradebin-cancel-order");
    const back = transfer(1, DEX, A, USDC, "5000000");
    expect(rendererFor(cancel.type_url).reasonFor!(back, cancel, dexCtx({ placed: [], fills: [], cancelled: [], pending: false }, [back]))).toBe(
      "returned from the cancelled order",
    );
  });
});

function ibcRow(t: Partial<IbcTransfer>): IbcTransfer {
  return {
    channel_id: "channel-3",
    sequence: 77,
    direction: "out",
    status: "pending",
    expired: false,
    sender: A,
    receiver: "cosmos1k4yvd7n5r9zxq4ta6l2h3g0wm8s0e7jcwf7fha",
    denom: ATOM,
    amount: "2000000",
    memo: null,
    counterparty: {
      chain_id: "cosmoshub-4",
      pretty_name: "Cosmos Hub",
      logo_url: null,
      explorer_account_url: "https://www.mintscan.io/cosmos/address/${accountAddress}",
      explorer_tx_url: null,
    },
    created_height: 25120440,
    created_tx_hash: "AB".repeat(32),
    created_time: "2026-10-11T09:15:02Z",
    timeout_height: null,
    timeout_time: "2026-10-11T10:15:02Z",
    result_height: null,
    result_tx_hash: null,
    result_time: null,
    ack_error: null,
    ...t,
  };
}

const ibcCtx = (rows: IbcTransfer[]) => createRenderContext({ transfers: [], tokens, ibcTransfers: rows });

describe("cross-chain status", () => {
  const send = fixture("ibc-transfer");

  it.each([
    ["pending", ibcRow({}), "Pending", "waiting for Cosmos Hub to confirm"],
    ["expired", ibcRow({ expired: true }), "Pending", "Expired, waiting for the refund"],
    ["delivered", ibcRow({ status: "delivered", result_tx_hash: "CD".repeat(32), result_time: "2026-10-11T09:16:00Z" }), "Delivered", "received on Cosmos Hub"],
    [
      "refunded-rejected",
      ibcRow({ status: "refunded_rejected", ack_error: "invalid receiver", result_tx_hash: "CD".repeat(32) }),
      "Refunded",
      "rejected by Cosmos Hub: invalid receiver",
    ],
    ["refunded-timeout", ibcRow({ status: "refunded_timeout", result_tx_hash: "CD".repeat(32) }), "Refunded", "timed out; the tokens are back in your account"],
  ])("%s", (_, row, chip, detail) => {
    const ctx = ibcCtx([row]);
    expect(rendererFor(send.type_url).title(send, ctx)).toBe("Cross-chain transfer to Cosmos Hub · 2 ATOM");
    body(send, ctx);
    const status = screen.getByTestId("ibc-status");
    expect(status).toHaveTextContent(chip);
    expect(status).toHaveTextContent(detail);
    if (row.result_tx_hash) {
      expect(within(status).getByRole("link", { name: "CDCD…CDCD" })).toHaveAttribute("href", `/txs/${row.result_tx_hash}`);
    }
    expect(within(status).getByRole("link", { name: /See on Cosmos Hub's explorer/ })).toHaveAttribute(
      "href",
      `https://www.mintscan.io/cosmos/address/${row.receiver}`,
    );
  });

  it("matches a transfer by its packet's sequence", () => {
    const withSeq = { ...send, events: [{ type: "send_packet", attrs: { packet_sequence: "78", packet_src_channel: "channel-3" } }] };
    const ctx = ibcCtx([ibcRow({}), ibcRow({ sequence: 78, status: "delivered" })]);
    body(withSeq, ctx);
    expect(screen.getByTestId("ibc-status")).toHaveAttribute("data-status", "delivered");
  });

  it("shows an inbound transfer the chain rejected", () => {
    const recv = fixture("ibc-recv-packet");
    const row = ibcRow({
      channel_id: "channel-0",
      sequence: 812,
      direction: "in",
      status: "rejected",
      ack_error: "blocked inbound denom",
      sender: "noble1x0d8qjxk3l4n7c5s6v2y9w4m8r0a3e5u7t6h2g",
      receiver: B,
      denom: "uusdc",
      amount: "320000000",
      counterparty: { chain_id: "noble-1", pretty_name: "Noble", logo_url: null, explorer_account_url: null, explorer_tx_url: null },
    });
    const ctx = ibcCtx([row]);
    expect(rendererFor(recv.type_url).title(recv, ctx)).toBe("Cross-chain transfer from Noble · 320000000 uusdc to bze1m3k…9ds4");
    body(recv, ctx);
    expect(screen.getByTestId("ibc-status")).toHaveTextContent("Rejected");
    expect(screen.getByTestId("ibc-status")).toHaveTextContent("blocked inbound denom");
  });

  it("keeps the plain title without a status (a list's context)", () => {
    expect(rendererFor(send.type_url).title(send, ibcCtx([]))).toBe("Cross-chain transfer · 2 ATOM → cosmos1k4y…7fha through channel-3");
  });
});
