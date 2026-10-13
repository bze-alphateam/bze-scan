import { describe, expect, it } from "vitest";

import type { Page, Token, TokenSummary } from "@/api/types";

import list from "../../test/fixtures/api/v1/tokens.json";
import honeyJson from "../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney.json";
import usdcJson from "../../test/fixtures/api/v1/tokens/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4.json";

import { connectionChip, headerChips, strandedExplainer, tokenBadges } from "./badges";
import { isObsolete } from "./tokens";

const honey = honeyJson as Token;
const usdc = usdcJson as Token;

const tokens = (list as Page<TokenSummary>).items;
const bySymbol = (s: string) => tokens.find((t) => t.symbol === s)!;
const texts = (t: TokenSummary) => tokenBadges(t).map((b) => `${b.kind}:${b.text}`);
const osmosis = { chain_id: "osmosis-1", name: "Osmosis", logo_url: null, status: "live" };
const vidulum = { chain_id: "vidulum-1", name: "Vidulum", logo_url: null, status: "killed" };

describe("token badges", () => {
  it("names how each token was born, Other for an unknown kind", () => {
    expect(tokens.map(texts)).toEqual([
      ["kind:Native"],
      ["kind:Factory"],
      ["kind:Factory"],
      ["kind:Cross-chain", "direct:Cosmos Hub"],
      ["kind:Cross-chain", "direct:Noble"],
      ["kind:DEX pool share"],
    ]);
    expect(texts({ ...tokens[0], kind: "unknown" })).toEqual(["kind:Other"]);
    for (const t of tokens) {
      for (const b of tokenBadges(t)) expect(b.explainer, b.text).not.toBe("");
    }
  });

  it("draws each kind and a one-hop token's chain in its own look", () => {
    expect(tokens.map((t) => tokenBadges(t).map((b) => b.look))).toEqual([["acc"], ["default"], ["default"], ["default", "ch"], ["default", "ch"], ["default"]]);
  });

  it("shows a multi-hop token's home chain, the chain it came through dashed, then the hops", () => {
    const twoHop = { ...bySymbol("ATOM"), ibc_hops: 2, via_chain: osmosis };
    const badges = tokenBadges(twoHop);
    expect(badges.map((b) => [b.kind, b.text, b.look])).toEqual([
      ["kind", "Cross-chain", "default"],
      ["home", "from Cosmos Hub", "ch"],
      ["direct", "via Osmosis", "via"],
      ["hops", "2 hops", "ln"],
    ]);
    expect(badges[1].explainer).toBe("ATOM was minted on Cosmos Hub, then travelled through Osmosis before reaching BZE.");
    expect(badges[2].explainer).toBe("BZE received ATOM from Osmosis, which it had reached from its home chain first.");
    expect(badges[3].explainer).toContain("not interchangeable");
  });

  it("highlights a stranded token and says why", () => {
    const vdl = {
      ...bySymbol("ATOM"),
      symbol: "VDL",
      origin_chain: vidulum,
      ibc_channel: { channel_id: "channel-1", state: "open", client_status: "expired" },
      stranded: true,
    };
    const badges = tokenBadges(vdl);
    expect(badges.map((b) => `${b.kind}:${b.text}`)).toEqual(["kind:Cross-chain", "direct:Vidulum (shut down)", "stranded:Stranded"]);
    expect(badges[2].look).toBe("warn");
    expect(badges[2].explainer).toBe(
      "Vidulum, where it was minted, has shut down. " +
        "BZE's connection to Vidulum (channel-1) has expired: no relayer updated it in time, so nothing crosses it until a governance proposal revives it. " +
        "The VDL on BZE can never be sent back; it can still move between BZE accounts.",
    );
  });

  it("explains each reason a token is stranded", () => {
    const atom = bySymbol("ATOM");
    const at = (ch: Partial<NonNullable<TokenSummary["ibc_channel"]>>) =>
      strandedExplainer({ ...atom, stranded: true, ibc_channel: { channel_id: "channel-8", state: "open", client_status: "active", ...ch } });
    expect(at({ client_status: "expired" })).toBe(
      "BZE's connection to Cosmos Hub (channel-8) has expired: no relayer updated it in time, so nothing crosses it until a governance proposal revives it. " +
        "Until then, the ATOM on BZE cannot be sent back; it can still move between BZE accounts.",
    );
    expect(at({ client_status: "frozen" })).toContain("has been frozen");
    expect(at({ state: "closed" })).toContain("BZE's channel to Cosmos Hub (channel-8) is closed.");
    const viaDead: TokenSummary = { ...atom, stranded: true, ibc_hops: 2, via_chain: vidulum };
    expect(strandedExplainer(viaDead)).toContain(
      "Vidulum, the chain it came through, has shut down.",
    );
    expect(strandedExplainer(atom)).toBeNull();
    expect(strandedExplainer({ ...bySymbol("BZE"), stranded: true })).toBeNull();
  });

  it("marks an obsolete token in red, last", () => {
    const corey = { ...bySymbol("HONEY"), denom: "factory/bze1f0qgels0eu96ev6a67znu70q7rquy9eragn8nw/ucorey" };
    const badges = tokenBadges(corey);
    expect(badges.map((b) => b.kind)).toEqual(["kind", "obsolete"]);
    expect(badges[1]).toMatchObject({ text: "Obsolete", look: "bad" });
  });
});

describe("token page chips", () => {
  it("adds the DEX state to the origin badges", () => {
    expect(headerChips(honey).map((c) => c.text)).toEqual(["Factory", "Trading on the DEX"]);
    expect(headerChips(usdc).map((c) => c.text)).toEqual(["Cross-chain", "Noble", "Trading on the DEX"]);
    expect(headerChips(usdc)[2].look).toBe("ok");
    const halted = headerChips({ ...usdc, halted: true });
    expect(halted.map((c) => c.text)).toEqual(["Cross-chain", "Noble", "Halted on the DEX"]);
    expect(halted[2]).toMatchObject({ look: "bad", explainer: expect.stringContaining("halted") });
    expect(headerChips({ ...honey, kind: "native", markets: [] }).map((c) => c.text)).toEqual(["Native"]);
    const corey = "factory/bze1f0qgels0eu96ev6a67znu70q7rquy9eragn8nw/ucorey";
    expect(headerChips({ ...honey, denom: corey }).map((c) => c.text)).toEqual(["Factory", "Obsolete", "Trading on the DEX"]);
    expect(isObsolete(corey)).toBe(true);
    expect(isObsolete(honey.denom)).toBe(false);
  });
});

describe("connection", () => {
  it("says whether BZE's channel still carries transfers, as a chip and the words after it", () => {
    const open = { channel_id: "channel-3", state: "open", client_status: "active" };
    expect(connectionChip(open)).toEqual({ look: "ok", text: "Open", note: "and active" });
    expect(connectionChip({ ...open, client_status: "expired" })).toMatchObject({ look: "warn", text: "Expired", note: expect.stringContaining("no relayer") });
    expect(connectionChip({ ...open, client_status: "frozen" })).toMatchObject({ look: "warn", text: "Frozen" });
    expect(connectionChip({ ...open, state: "closed" })).toMatchObject({ look: "bad", text: "Closed" });
    expect(connectionChip({ ...open, client_status: null })).toEqual({ look: "default", text: "Open", note: "" });
    expect(connectionChip({ ...open, state: null, client_status: null }).text).toBe("Not known yet");
  });
});
