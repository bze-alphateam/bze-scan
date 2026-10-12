import { describe, expect, it } from "vitest";

import type { Page, Token, TokenEvent, TokenHolder, TokenSummary, TokenTransfer } from "@/api/types";

import honeyJson from "../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney.json";
import holdersJson from "../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney/holders.json";
import transfersJson from "../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney/transfers.json";
import usdcJson from "../../test/fixtures/api/v1/tokens/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4.json";
import list from "../../test/fixtures/api/v1/tokens.json";

import {
  activityItems,
  adminNote,
  baseUnitText,
  changeText,
  denomFromSegments,
  DEX_MODULE_ADDRESS,
  eventLine,
  filterTokens,
  headerChips,
  hopsExplainer,
  hopsText,
  ibcRoute,
  isObsolete,
  listOriginTag,
  mergeEvents,
  mintBurnText,
  noPriceText,
  priceText,
  supplyState,
  supplyTileSub,
  tokenHref,
  tokensTiles,
  topHoldersText,
  transferLine,
} from "./tokens";

const tokens = (list as Page<TokenSummary>).items;
const honey = honeyJson as Token;
const usdc = usdcJson as Token;
const holders = (holdersJson as Page<TokenHolder>).items;
const transfers = (transfersJson as Page<TokenTransfer>).items;
const bySymbol = (s: string) => tokens.find((t) => t.symbol === s)!;
const now = new Date("2099-10-11T10:00:00Z");

describe("tokens list", () => {
  it("counts the tiles from the whole list", () => {
    expect(tokensTiles(tokens, now)).toEqual({
      count: 6,
      byKind: "1 native · 2 Factory · 2 cross-chain · 1 pool share",
      onDex: 4,
      createdThisMonth: 1,
      chains: ["Cosmos Hub", "Noble"],
    });
    expect(tokensTiles(tokens, new Date("2099-11-01T00:00:00Z")).createdThisMonth).toBe(0);
  });

  it("tags each row's origin by kind, Other for an unknown one", () => {
    expect(tokens.map((t) => [t.symbol, listOriginTag(t)])).toEqual([
      ["BZE", "Native"],
      ["HONEY", "Factory"],
      ["VDL", "Factory"],
      ["ATOM", "Cross-chain · Cosmos Hub"],
      ["USDC", "Cross-chain · Noble"],
      ["BZE/USDC LP", "DEX pool share"],
    ]);
    expect(listOriginTag({ ...tokens[0], kind: "unknown" })).toBe("Other");
  });

  it("tells a multi-hop token from the one sent straight from home", () => {
    const osmosis = { chain_id: "osmosis-1", name: "Osmosis", logo_url: null };
    const direct = bySymbol("ATOM");
    const twoHop = { ...direct, ibc_hops: 2, via_chain: osmosis };
    expect(listOriginTag(direct)).toBe("Cross-chain · Cosmos Hub");
    expect(listOriginTag(twoHop)).toBe("Cross-chain · Cosmos Hub via Osmosis");
    expect(hopsText({ hops: 2, via: "Osmosis" })).toBe("2 hops via Osmosis");
    expect(hopsText({ hops: 3, via: null })).toBe("3 hops");
    expect(hopsExplainer(direct)).toBeNull();
    expect(hopsExplainer(twoHop)).toBe(
      "This ATOM came to BZE through Osmosis (2 hops), not straight from Cosmos Hub. " +
        "It is a different token from the ATOM sent directly from Cosmos Hub: the two are not interchangeable on BZE. " +
        "To swap one for the other, send it back the way it came.",
    );
    expect(hopsExplainer({ ...twoHop, via_chain: null })).toContain("through another chain (2 hops)");
  });

  it("traces an IBC token's route from its home chain to BZE", () => {
    const injective = { chain_id: "injective-1", name: "Injective", logo_url: "inj.png" };
    const hub = { chain_id: "cosmoshub-4", name: "Cosmos Hub", logo_url: "hub.png" };
    const base = { kind: "ibc", origin_chain: injective, origin_chain_id: "injective-1", via_chain: null };
    expect(ibcRoute({ ...base, ibc_path: "transfer/channel-13" })).toEqual({
      chains: [
        { name: "Injective", logoUrl: "inj.png" },
        { name: "BZE", logoUrl: null },
      ],
      hops: [{ channel: "transfer/channel-13", on: "BZE", toward: "Injective" }],
    });
    expect(ibcRoute({ ...base, via_chain: hub, ibc_path: "transfer/channel-11/transfer/channel-220" })).toEqual({
      chains: [
        { name: "Injective", logoUrl: "inj.png" },
        { name: "Cosmos Hub", logoUrl: "hub.png" },
        { name: "BZE", logoUrl: null },
      ],
      hops: [
        { channel: "transfer/channel-11", on: "BZE", toward: "Cosmos Hub" },
        { channel: "transfer/channel-220", on: "Cosmos Hub", toward: "Injective" },
      ],
    });
    // Beyond the first hop the explorer knows only the home chain.
    expect(ibcRoute({ ...base, via_chain: hub, ibc_path: "transfer/channel-1/transfer/channel-2/transfer/channel-3" })?.hops).toEqual([
      { channel: "transfer/channel-1", on: "BZE", toward: "Cosmos Hub" },
      { channel: "transfer/channel-2", on: "Cosmos Hub", toward: "another chain" },
      { channel: "transfer/channel-3", on: "another chain", toward: "Injective" },
    ]);
    expect(ibcRoute({ ...base, ibc_path: null })).toBeNull();
    expect(ibcRoute({ ...base, ibc_path: "transfer" })).toBeNull();
    expect(ibcRoute({ ...base, kind: "factory", ibc_path: "transfer/channel-1" })).toBeNull();
  });

  it("filters by chip, then on symbol or name", () => {
    const symbols = (rows: TokenSummary[]) => rows.map((t) => t.symbol);
    expect(symbols(filterTokens(tokens, "factory", ""))).toEqual(["HONEY", "VDL"]);
    expect(symbols(filterTokens(tokens, "ibc", ""))).toEqual(["ATOM", "USDC"]);
    expect(symbols(filterTokens(tokens, "all", " coin "))).toEqual(["USDC"]);
    expect(symbols(filterTokens(tokens, "native", "hon"))).toEqual([]);
    expect(filterTokens(tokens, "all", "")).toHaveLength(6);
  });

  it("words the price cell", () => {
    expect(priceText("0.0015")).toBe("$0.0015");
    expect(priceText("1")).toBe("$1.00");
    expect(changeText("1.2345")).toEqual({ text: "+1.2%", tone: "green" });
    expect(changeText("-2.1362")).toEqual({ text: "-2.1%", tone: "red" });
    expect(changeText(null)).toBeNull();
    expect(noPriceText(bySymbol("ATOM"))).toBe("— Halted on the DEX");
    expect(noPriceText(bySymbol("VDL"))).toBe("— no market");
    expect(noPriceText(bySymbol("HONEY"))).toBeNull();
  });

  it("round-trips a denom through its URL", () => {
    for (const denom of [honey.denom, usdc.denom, "ubze", "ulp_ubze_ibc/6490"]) {
      const segment = tokenHref(denom).slice("/tokens/".length);
      expect(segment).not.toContain("/");
      expect(denomFromSegments([segment])).toBe(denom);
      expect(denomFromSegments(denom.split("/"))).toBe(denom);
    }
    expect(tokenHref(honey.denom)).toBe("/tokens/factory%2Fbze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk%2Fuhoney");
    expect(denomFromSegments(["%E0%A4%A"])).toBeNull();
  });
});

describe("token page", () => {
  it("chips the origin and the DEX state", () => {
    expect(headerChips(honey).map((c) => c.text)).toEqual(["Created on Factory", "Trading on the DEX"]);
    expect(headerChips(usdc).map((c) => c.text)).toEqual(["Cross-chain from Noble", "Trading on the DEX"]);
    expect(headerChips({ ...usdc, halted: true }).map((c) => c.text)).toEqual(["Cross-chain from Noble", "Halted on the DEX"]);
    expect(headerChips({ ...honey, kind: "native", markets: [] }).map((c) => c.text)).toEqual(["Native"]);
    const corey = "factory/bze1f0qgels0eu96ev6a67znu70q7rquy9eragn8nw/ucorey";
    const obsolete = headerChips({ ...honey, denom: corey });
    expect(obsolete.map((c) => c.text)).toEqual(["Created on Factory", "Obsolete", "Trading on the DEX"]);
    expect(obsolete[1].title).toContain("no longer list this token");
    expect(isObsolete(corey)).toBe(true);
    expect(isObsolete(honey.denom)).toBe(false);
    const twoHop = headerChips({ ...usdc, ibc_hops: 2, via_chain: { chain_id: "osmosis-1", name: "Osmosis", logo_url: "o.png" } });
    expect(twoHop.map((c) => c.text)).toEqual(["Cross-chain from Noble", "2 hops via Osmosis", "Trading on the DEX"]);
    expect(twoHop[1]).toMatchObject({ tone: "orange", logoUrl: "o.png" });
    expect(twoHop[1].title).toContain("not interchangeable");
  });

  it("says whether the supply can change", () => {
    expect(supplyState(honey)).toBe("admin");
    expect(supplyState({ ...honey, admin: null })).toBe("fixed");
    expect(supplyState(usdc)).toBe("elsewhere");
    expect(supplyState({ kind: "native", admin: null })).toBe("inflation");
    expect(supplyState({ kind: "lp", admin: null })).toBe("pool");
    expect(supplyState({ kind: "unknown", admin: null })).toBeNull();
    expect(supplyTileSub("admin")).toBe("can change · see below");
    expect(supplyTileSub("fixed")).toBe("fixed");
  });

  it("totals the mints and burns from the events", () => {
    expect(mintBurnText(honey.events, honey)).toBe("So far: 2 mints adding 1,000,500 HONEY, 1 burn removing 200 HONEY.");
    expect(mintBurnText(honey.events.filter((e) => e.kind !== "burned"), honey)).toBe(
      "So far: 2 mints adding 1,000,500 HONEY.",
    );
    expect(mintBurnText([], honey)).toBe("So far it has not minted or burned any.");
  });

  it("words the admin and the base unit", () => {
    expect(adminNote(honey)).toBe("same account · can mint, burn and change the branding");
    expect(adminNote({ ...honey, admin: "bze1other" })).toBe("can mint, burn and change the branding");
    expect(adminNote({ ...honey, admin: null })).toBe("renounced: nobody can mint or burn");
    expect(baseUnitText(honey)).toBe("1 HONEY = 1,000,000 uhoney");
    expect(baseUnitText(usdc)).toBe("1 USDC = 1,000,000 uusdc");
    expect(baseUnitText({ ...honey, kind: "unknown", denom: "uzzz", symbol: null, exponent: 0 })).toBe("1 uzzz = 1 uzzz");
  });

  it("sums the top five holders' share", () => {
    expect(topHoldersText(holders)).toBe("the top 5 hold 87.5%");
    expect(topHoldersText(holders.slice(0, 2))).toBe("the top 2 hold 70.0%");
    expect(topHoldersText([])).toBeNull();
  });

  it("words the token events", () => {
    const [market, burned, minted, , created] = honey.events;
    expect(eventLine(minted, honey)).toEqual({ text: "Minted 500 HONEY by the admin", party: null });
    expect(eventLine(burned, honey)).toEqual({ text: "Burned 200 HONEY by the admin", party: null });
    expect(eventLine(minted, { ...honey, admin: null })).toEqual({
      text: "Minted 500 HONEY by",
      party: { address: honey.creator, label: null },
    });
    expect(eventLine(created, honey)).toEqual({ text: "Created by", party: { address: honey.creator, label: null } });
    expect(eventLine(market, honey).text).toBe("Market created on the DEX");
    const change = (to: string): TokenEvent => ({ ...created, kind: "admin_changed", details: { from: honey.creator, to } });
    expect(eventLine(change("bze1new"), honey)).toEqual({ text: "Admin changed to", party: { address: "bze1new", label: null } });
    expect(eventLine(change(""), honey)).toEqual({ text: "Admin renounced: the supply is now fixed", party: null });
    expect(eventLine({ ...created, kind: "halted", actor: null }, honey).text).toBe("Halted on the DEX by governance");
  });

  it("words a transfer as a send, a trade or a mint", () => {
    const [sent, traded, minted] = transfers;
    expect(transferLine(sent, honey)).toEqual({
      kind: "sent",
      amount: "25,000 HONEY",
      from: { address: sent.sender, label: null },
      to: { address: sent.recipient, label: null },
    });
    expect(traded.sender).toBe(DEX_MODULE_ADDRESS);
    expect(transferLine(traded, honey)).toEqual({ kind: "traded", amount: "75,000 HONEY", party: { address: traded.recipient, label: null } });
    expect(transferLine(minted, honey)).toMatchObject({ kind: "minted", amount: "500 HONEY" });
  });

  it("merges events and transfers newest first, a mint told once", () => {
    const keys = (items: ReturnType<typeof activityItems>) =>
      items.map((i) => (i.type === "event" ? i.event.kind : `transfer ${i.transfer.height}`));
    expect(keys(activityItems(honey.events, transfers, false))).toEqual([
      "transfer 24001000",
      "transfer 24000950",
      "market_created",
      "burned",
      "minted",
      "created",
      "minted",
    ]);
    // While older transfers remain, older events wait for them.
    expect(keys(activityItems(honey.events, transfers.slice(0, 2), true))).toEqual(["transfer 24001000", "transfer 24000950"]);
    expect(keys(activityItems([], [], false))).toEqual([]);
  });

  it("merges the newest events with the older pages once", () => {
    const older = [honey.events[4], { ...honey.events[4], height: 23000000, seq: 0 }];
    const merged = mergeEvents(honey.events.slice(0, 5), older);
    expect(merged).toHaveLength(6);
    expect(merged.at(-1)!.height).toBe(23000000);
    expect(mergeEvents(honey.events, undefined)).toHaveLength(5);
  });
});
