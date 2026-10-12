import { describe, expect, it } from "vitest";

import type { Account, Page, TokenSummary, Validator } from "@/api/types";

import account from "../../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4.json";
import tokens from "../../test/fixtures/api/v1/tokens.json";
import list from "../../test/fixtures/api/v1/validators.json";

import {
  accountTitle,
  activeSince,
  daysText,
  daysUntil,
  dexAssetUrl,
  labelChip,
  originTag,
  parseTab,
  stakingRows,
  tokenRows,
  tokensTotalUsd,
  unlockText,
  unstaking,
  validatorStatus,
} from "./account";

const a = account as Account;
const denoms = new Map((tokens as Page<TokenSummary>).items.map((t) => [t.denom, t]));
const byDenom = (d: string) => denoms.get(d);
const validators = list.items as Validator[];
const now = new Date("2099-10-11T10:00:00Z");

describe("header", () => {
  it("names a plain account by its short address and a labelled one by its label", () => {
    expect(accountTitle(a.address, null)).toBe("Account bze1m3k…9ds4");
    expect(accountTitle(a.address, { name: "Community pool", kind: "module" })).toBe("Community pool");
  });

  it("gives each label kind its chip", () => {
    expect(labelChip(null)).toBeNull();
    expect(labelChip({ name: "distribution", kind: "module" })).toEqual({ text: "module account", dashed: true });
    expect(labelChip({ name: "Honeycomb", kind: "validator_owner" })).toEqual({ text: "owner of Honeycomb", dashed: false });
    expect(labelChip({ name: "channel-0 escrow", kind: "ibc_escrow" })).toEqual({ text: "cross-chain escrow", dashed: false });
    expect(labelChip({ name: "BZE team", kind: "known" })).toEqual({ text: "BZE team", dashed: false });
  });

  it("says since when the account is active, or that it was never seen", () => {
    expect(activeSince(a)).toBe("active since Apr 2022 · 1,204 transactions");
    expect(activeSince({ first_seen: { height: 1, time: "2025-06-02T12:00:00Z" }, tx_count: 1 })).toBe(
      "active since Jun 2025 · 1 transaction",
    );
    expect(activeSince({ first_seen: null, tx_count: 0 })).toBe("no transactions seen yet");
  });

  it("reads the tab from the query, the Activity tab by default", () => {
    expect(parseTab("tokens")).toBe("tokens");
    expect(parseTab("staking")).toBe("staking");
    expect(parseTab(undefined)).toBe("activity");
    expect(parseTab("nonsense")).toBe("activity");
    expect(parseTab(["tokens", "staking"])).toBe("activity");
  });
});

describe("unstaking", () => {
  it("sums the entries and counts the days to the earliest unlock", () => {
    expect(unstaking(a.unbonding, now)).toEqual({ total: "300000000", nextDays: 9 });
    expect(unstaking([], now)).toEqual({ total: "0", nextDays: null });
  });

  it("rounds a part day up and says today once passed", () => {
    expect(daysUntil("2099-10-11T10:00:01Z", now)).toBe(1);
    expect(daysUntil("2099-10-01T00:00:00Z", now)).toBe(0);
    expect(daysText(1)).toBe("1 day");
    expect(daysText(19)).toBe("19 days");
    expect(daysText(0)).toBe("today");
  });

  it("words an entry's unlock", () => {
    expect(unlockText(a.unbonding[0], now)).toBe("back in your account on Oct 30, 2099, 10:00:00 UTC (19 days)");
  });
});

describe("tokens tab", () => {
  it("tags each origin by kind, the raw denom for an unknown one", () => {
    expect(originTag(byDenom("ubze"), "ubze")).toBe("Native");
    expect(originTag(byDenom(a.balances[1].denom), a.balances[1].denom)).toBe("Factory");
    expect(originTag(byDenom(a.balances[2].denom), a.balances[2].denom)).toBe("Cross-chain · Noble");
    expect(originTag(byDenom(a.balances[4].denom), a.balances[4].denom)).toBe("DEX pool share");
    expect(originTag(undefined, "uzzz")).toBe("uzzz");
    const noName = { ...byDenom(a.balances[2].denom)!, origin_chain: { chain_id: "noble-1", name: null, logo_url: null, status: null } };
    expect(originTag(noName, noName.denom)).toBe("Cross-chain · noble-1");
  });

  it("joins the non-zero balances to the tokens list and values them at DEX prices", () => {
    const rows = tokenRows(a.balances, denoms, "value");
    expect(rows.map((r) => [r.symbol ?? r.denom, r.valueUsd])).toEqual([
      ["USDC", "25.5"],
      ["HONEY", "10"],
      ["BZE", "1.85175"],
      ["BZE/USDC LP", null],
      ["uzzz", null],
    ]);
    expect(rows[0]).toMatchObject({ name: "USD Coin", origin: "Cross-chain · Noble", exponent: 6 });
    expect(tokensTotalUsd(rows)).toBe("37.35175");
  });

  it("sorts by name on request", () => {
    expect(tokenRows(a.balances, denoms, "name").map((r) => r.symbol ?? r.denom)).toEqual([
      "BZE",
      "BZE/USDC LP",
      "HONEY",
      "USDC",
      "uzzz",
    ]);
  });

  it("falls back to the balance's own display data without the tokens list", () => {
    const rows = tokenRows(a.balances, undefined, "value");
    expect(rows).toHaveLength(5);
    expect(rows.every((r) => r.valueUsd === null)).toBe(true);
    expect(rows.find((r) => r.denom === "ubze")).toMatchObject({ symbol: "BZE", exponent: 6, origin: "ubze" });
  });

  it("links a token to its DEX page with the denom encoded", () => {
    expect(dexAssetUrl("factory/bze1x/uhoney")).toBe("https://dex.getbze.com/assets/details?denom=factory%2Fbze1x%2Fuhoney");
  });
});

describe("staking tab", () => {
  it("gives each delegation its share of the stake and its rewards", () => {
    expect(stakingRows(a).map((r) => [r.moniker, r.share, r.rewards])).toEqual([
      ["Honeycomb", "60%", "12500000"],
      ["Deep Fields", "30%", "3250000"],
      ["Sleepy Node", "10%", null],
    ]);
    expect(stakingRows({ ...a, total_staked: "0" })[0].share).toBe("—");
  });

  it("chips a validator Active, Inactive or Jailed, with its uptime under 99%", () => {
    const [honeycomb, , deep, sleepy, old] = validators;
    expect(validatorStatus(honeycomb)).toEqual({ text: "Active", tone: "green", uptime: null });
    expect(validatorStatus(deep)).toEqual({ text: "Active", tone: "green", uptime: "98.4% uptime" });
    expect(validatorStatus(sleepy)).toEqual({ text: "Jailed", tone: "red", uptime: "61.2% uptime" });
    expect(validatorStatus(old)).toEqual({ text: "Inactive", tone: "gray", uptime: null });
    expect(validatorStatus(undefined)).toBeNull();
  });
});
