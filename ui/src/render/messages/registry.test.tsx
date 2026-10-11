import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";

import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Transfer } from "@/api/types";
import { renderWithProviders } from "@/test/render";

import { fallbackRenderer } from "./fallback";
import { createRenderContext, registry, rendererFor } from "./registry";
import type { RenderMessage } from "./types";

// vitest runs from ui/.
const DIR = path.resolve("test/fixtures/messages") + path.sep;

function fixture(name: string): RenderMessage {
  return { msg_index: 0, ...JSON.parse(readFileSync(`${DIR}${name}.json`, "utf8")) };
}

const A = "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw";
const B = "bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4";
const V1 = "bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c";
const V2 = "bzevaloper1x7ym3n6d9kjwz0q4u5r2a8l4tqs9vfmyh2gz3e";
const ATOM = "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2";
const USDC = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4";

const transfers: Transfer[] = [
  {
    seq: 0,
    msg_index: 0,
    kind: "mint",
    sender: null,
    sender_label: null,
    recipient: B,
    recipient_label: null,
    denom: USDC,
    amount: "320000000",
    symbol: "USDC",
    exponent: 6,
  },
];

const ctx = createRenderContext({
  transfers,
  tokens: new Map([
    ["ubze", { symbol: "BZE", exponent: 6, price_usd: "0.0015" }],
    [ATOM, { symbol: "ATOM", exponent: 6, price_usd: null }],
  ]),
  validators: new Map([
    [V1, { moniker: "Honeycomb" }],
    [V2, { moniker: "Thamar Node" }],
  ]),
});

// Every fixture in test/fixtures/messages and the title it must read as.
const TITLES: Record<string, string> = {
  "bank-send": "Send 10 BZE to bze1m3k…9ds4",
  "bank-multi-send": "Send to 2 recipients",
  "staking-delegate": "Stake 500 BZE with Honeycomb",
  "staking-undelegate": "Unstake 500 BZE from Honeycomb · back on Oct 27, 2026",
  "staking-begin-redelegate": "Move 250 BZE of stake from Honeycomb to Thamar Node",
  "staking-cancel-unbonding": "Cancel unstaking 100 BZE from Honeycomb",
  "staking-create-validator": "Create validator Honeycomb",
  "staking-edit-validator": "Edit validator Honeycomb",
  "slashing-unjail": "Unjail Honeycomb",
  "distribution-withdraw-reward": "Claim staking rewards from Honeycomb",
  "distribution-withdraw-commission": "Claim the commission of Honeycomb",
  "distribution-set-withdraw-address": "Send future staking rewards to bze1m3k…9ds4",
  "distribution-fund-community-pool": "Fund the community pool with 100 BZE",
  "gov-submit-proposal": "Submit proposal #14: Raise the block size",
  "gov-submit-proposal-v1beta1": "Submit proposal #15: Community call",
  "gov-deposit": "Deposit 1,000 BZE on proposal #14",
  "gov-vote": "Vote Yes on proposal #14",
  "gov-vote-weighted": "Vote Yes 70%, No 30% on proposal #14",
  "ibc-transfer": "Cross-chain transfer · 2 ATOM → cosmos1k4y…7fha through channel-3",
  "ibc-recv-packet": "Cross-chain transfer received: 320 USDC to bze1m3k…9ds4",
  "ibc-acknowledgement": "Cross-chain delivery confirmed",
  "ibc-timeout": "Cross-chain transfer timed out",
  "ibc-update-client": "Cross-chain client update",
  "ibc-channel-open-init": "Cross-chain channel setup",
  "authz-grant": "Allow bze1m3k…9ds4 to vote",
  "authz-revoke": "Stop allowing bze1m3k…9ds4 to delegate",
  "authz-exec": "Run 2 actions with a permission",
  "authz-exec-single": "Vote No on proposal #14",
  "feegrant-grant-allowance": "Pay the fees of bze1m3k…9ds4",
  "feegrant-revoke-allowance": "Stop paying the fees of bze1m3k…9ds4",
  fallback: "Create denom reward",
  "fallback-undecoded": "From the future",
};

const names = readdirSync(DIR)
  .filter((f) => f.endsWith(".json"))
  .map((f) => f.slice(0, -".json".length));

describe("message renderers", () => {
  it("has an expected title for every fixture", () => {
    expect(names.sort()).toEqual(Object.keys(TITLES).sort());
  });

  it.each(names)("%s reads in plain words and renders its body", (name) => {
    const msg = fixture(name);
    const r = rendererFor(msg.type_url);
    expect(r.title(msg, ctx)).toBe(TITLES[name]);
    expect(r.summary(msg, ctx)).not.toBe("");
    renderWithProviders(<r.Body msg={msg} ctx={ctx} />);
  });

  it("covers every registered type except the one-line relay and handshake messages with a fixture", () => {
    const covered = new Set(names.map((n) => fixture(n).type_url));
    const uncovered = [...registry.keys()].filter((t) => !covered.has(t) && !t.startsWith("/ibc.core."));
    // The v1beta1 gov aliases share their v1 renderer.
    expect(uncovered.filter((t) => !t.startsWith("/cosmos.gov.v1beta1."))).toEqual([]);
  });

  it("falls back for an unknown type", () => {
    expect(rendererFor("/bze.burner.MsgSomethingNew")).toBe(fallbackRenderer);
  });

  it("summarises a send for lists", () => {
    const msg = fixture("bank-send");
    expect(rendererFor(msg.type_url).summary(msg, ctx)).toBe("Send · 10 BZE → bze1m3k…9ds4");
  });

  it("shows a send's parties as chips and its USD value", () => {
    const msg = fixture("bank-send");
    const r = rendererFor(msg.type_url);
    renderWithProviders(<r.Body msg={msg} ctx={ctx} />);
    expect(screen.getByRole("link", { name: "bze18uf…9zhw" })).toHaveAttribute("href", `/accounts/${A}`);
    expect(screen.getByRole("link", { name: "bze1m3k…9ds4" })).toHaveAttribute("href", `/accounts/${B}`);
    expect(screen.getByText("10 BZE")).toBeInTheDocument();
    expect(screen.getByText("≈ $0.02")).toBeInTheDocument();
  });

  it("reads a claimed reward from the withdraw_rewards event", () => {
    const msg = fixture("distribution-withdraw-reward");
    const r = rendererFor(msg.type_url);
    renderWithProviders(<r.Body msg={msg} ctx={ctx} />);
    expect(screen.getByText("1.234567 BZE")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Honeycomb" })).toHaveAttribute("href", `/validators/${V1}`);
  });

  it("unwraps MsgExec's messages through the registry", () => {
    const msg = fixture("authz-exec");
    const r = rendererFor(msg.type_url);
    renderWithProviders(<r.Body msg={msg} ctx={ctx} />);
    const inner = screen.getAllByTestId("exec-inner");
    expect(inner).toHaveLength(2);
    expect(within(inner[0]).getByText("1. Claim staking rewards from Honeycomb")).toBeInTheDocument();
    expect(within(inner[1]).getByText("2. Stake 1.234567 BZE with Honeycomb")).toBeInTheDocument();
  });

  it("shows the fallback's body as a key/value table", () => {
    const msg = fixture("fallback");
    renderWithProviders(<fallbackRenderer.Body msg={msg} ctx={ctx} />);
    expect(screen.getByText("Prize denom")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "bze18uf…9zhw" })).toBeInTheDocument();
    expect(screen.getByText("30 BZE")).toBeInTheDocument();
    expect(screen.getByText(/"nested": true/)).toBeInTheDocument();
  });

  it("says so when the backend could not decode a body", () => {
    const msg = fixture("fallback-undecoded");
    renderWithProviders(<fallbackRenderer.Body msg={msg} ctx={ctx} />);
    expect(screen.getByText(/cannot read this action/)).toBeInTheDocument();
  });

  it("never crashes on a body missing every field", () => {
    for (const typeUrl of registry.keys()) {
      const msg: RenderMessage = { type_url: typeUrl, sender: null, module: null, body: {}, events: null, msg_index: 0 };
      const r = rendererFor(typeUrl);
      expect(typeof r.title(msg, ctx)).toBe("string");
      const { unmount } = renderWithProviders(<r.Body msg={msg} ctx={ctx} />);
      unmount();
    }
  });

  it("shows the raw amount and denom when the denom is unknown", () => {
    const msg = fixture("bank-send");
    const bare = createRenderContext({ transfers: [] });
    expect(rendererFor(msg.type_url).title(msg, bare)).toBe("Send 10000000 ubze to bze1m3k…9ds4");
  });
});
