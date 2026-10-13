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
  directToken,
  eventLine,
  filterTokens,
  hopsExplainer,
  ibcRoute,
  mergeEvents,
  mintBurnText,
  multiHopCallout,
  noPriceText,
  priceText,
  routeNote,
  shortDenom,
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

  it("tells a multi-hop token from the one sent straight from home", () => {
    const osmosis = { chain_id: "osmosis-1", name: "Osmosis", logo_url: null, status: null };
    const direct = bySymbol("ATOM");
    const twoHop = { ...direct, ibc_hops: 2, via_chain: osmosis };
    expect(hopsExplainer(direct)).toBeNull();
    expect(hopsExplainer(twoHop)).toBe(
      "This ATOM came to BZE through Osmosis (2 hops), not straight from Cosmos Hub. " +
        "It is a different token from the ATOM sent directly from Cosmos Hub: the two are not interchangeable on BZE. " +
        "To swap one for the other, send it back the way it came.",
    );
    expect(hopsExplainer({ ...twoHop, via_chain: null })).toContain("through another chain (2 hops)");
    // The amber callout says the same in a title and one line, only from two hops on.
    expect(multiHopCallout(direct)).toBeNull();
    expect(multiHopCallout(twoHop)).toEqual({
      title: "Not the same token as ATOM sent straight from Cosmos Hub",
      body:
        "It came through Osmosis, so BZE sees it as a different token from the ATOM that came directly: the two are not interchangeable on BZE. " +
        "To turn it into the direct one, send it back to Osmosis and on to Cosmos Hub, then over to BZE.",
    });
    expect(multiHopCallout({ ...twoHop, via_chain: null })?.body).toMatch(/^It came through another chain,/);
    // Past two hops the chains between are not all named: back the way it came.
    expect(multiHopCallout({ ...twoHop, ibc_hops: 3 })?.body).toMatch(/send it back the way it came, to Cosmos Hub, then over to BZE\.$/);
  });

  it("finds the token sent straight from a multi-hop token's home chain", () => {
    const osmosis = { chain_id: "osmosis-1", name: "Osmosis", logo_url: null, status: null };
    const direct = bySymbol("ATOM");
    const twoHop = { ...direct, denom: "ibc/TWOHOP", ibc_hops: 2, via_chain: osmosis };
    const tokens = [twoHop, { ...direct, origin_chain: osmosis }, direct];
    expect(directToken(twoHop, tokens)).toBe(direct);
    expect(directToken(direct, tokens)).toBeNull();
    expect(directToken(twoHop, [twoHop])).toBeNull();
    expect(directToken(twoHop, undefined)).toBeNull();
  });

  it("shortens a denom for a breadcrumb", () => {
    expect(shortDenom("factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney")).toBe("factory/bze1…/uhoney");
    expect(shortDenom("ibc/82931180F0962712BFABB189083A94C77EA90E269ABFC6D310AF922B2B14E011")).toBe("ibc/82931180…");
    expect(shortDenom("ubze")).toBe("ubze");
  });

  it("draws a one-hop token's route: home, then BZE over BZE's channel", () => {
    const injective = { chain_id: "injective-1", name: "Injective", logo_url: "inj.png", status: null };
    const base = { kind: "ibc", symbol: "INJ", denom: "ibc/INJ", origin_chain: injective, origin_chain_id: "injective-1", ibc_base_denom: "inj", via_chain: null };
    expect(ibcRoute({ ...base, ibc_path: "transfer/channel-13" })).toEqual({
      hops: 1,
      stops: [
        { name: "Injective", role: "origin", detail: "inj" },
        { name: "BZE", role: "here", detail: "INJ" },
      ],
      links: [{ channels: ["channel-13"], on: "BZE" }],
      paths: [{ channel: "transfer/channel-13", on: "BZE", toward: "Injective" }],
    });
    expect(routeNote(1)).toBe("direct · 1 hop");
    expect(ibcRoute({ ...base, ibc_path: null })).toBeNull();
    expect(ibcRoute({ ...base, ibc_path: "transfer" })).toBeNull();
    expect(ibcRoute({ ...base, kind: "factory", ibc_path: "transfer/channel-1" })).toBeNull();
    // Without a known home chain the route still says where it starts.
    expect(ibcRoute({ ...base, origin_chain: null, origin_chain_id: null, ibc_path: "transfer/channel-13" })?.stops[0].name).toBe("its home chain");
  });

  it("draws a two-hop token's route through the chain it came by, each link with that chain's channel", () => {
    const atomone = { chain_id: "atomone-1", name: "AtomOne", logo_url: null, status: null };
    const osmosis = { chain_id: "osmosis-1", name: "Osmosis", logo_url: null, status: null };
    const atone = {
      kind: "ibc",
      symbol: "ATONE",
      denom: "ibc/E7CF",
      origin_chain: atomone,
      origin_chain_id: "atomone-1",
      ibc_base_denom: "uatone",
      via_chain: osmosis,
      ibc_path: "transfer/channel-0/transfer/channel-94814",
    };
    expect(ibcRoute(atone)).toEqual({
      hops: 2,
      stops: [
        { name: "AtomOne", role: "origin", detail: "uatone" },
        { name: "Osmosis", role: "through", detail: null },
        { name: "BZE", role: "here", detail: "ATONE · via Osmosis" },
      ],
      links: [
        { channels: ["channel-94814"], on: "Osmosis" },
        { channels: ["channel-0"], on: "BZE" },
      ],
      paths: [
        { channel: "transfer/channel-0", on: "BZE", toward: "Osmosis" },
        { channel: "transfer/channel-94814", on: "Osmosis", toward: "AtomOne" },
      ],
    });
    expect(routeNote(2)).toBe("2 hops · not direct");
  });

  it("counts the chains it cannot name on a longer route, never inventing them", () => {
    const injective = { chain_id: "injective-1", name: "Injective", logo_url: null, status: null };
    const hub = { chain_id: "cosmoshub-4", name: "Cosmos Hub", logo_url: null, status: null };
    const base = { kind: "ibc", symbol: "INJ", denom: "ibc/INJ", origin_chain: injective, origin_chain_id: "injective-1", ibc_base_denom: "inj", via_chain: hub };
    const three = ibcRoute({ ...base, ibc_path: "transfer/channel-1/transfer/channel-2/transfer/channel-3" })!;
    expect(three.stops.map((s) => [s.name, s.role])).toEqual([
      ["Injective", "origin"],
      ["1 more chain", "counted"],
      ["Cosmos Hub", "through"],
      ["BZE", "here"],
    ]);
    expect(three.links).toEqual([
      { channels: ["channel-3"], on: "another chain" },
      { channels: ["channel-2"], on: "Cosmos Hub" },
      { channels: ["channel-1"], on: "BZE" },
    ]);
    expect(three.paths).toEqual([
      { channel: "transfer/channel-1", on: "BZE", toward: "Cosmos Hub" },
      { channel: "transfer/channel-2", on: "Cosmos Hub", toward: "another chain" },
      { channel: "transfer/channel-3", on: "another chain", toward: "Injective" },
    ]);
    const four = ibcRoute({ ...base, ibc_path: "transfer/channel-1/transfer/channel-2/transfer/channel-3/transfer/channel-4" })!;
    expect(four.stops[1].name).toBe("2 more chains");
    expect(four.links[0]).toEqual({ channels: ["channel-4", "channel-3"], on: "the chains between" });
    // An unknown chain it came through is not named either.
    expect(ibcRoute({ ...base, via_chain: null, ibc_path: "transfer/channel-1/transfer/channel-2" })!.stops[1].name).toBe("another chain");
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
