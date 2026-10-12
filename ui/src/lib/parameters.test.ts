import { describe, expect, it } from "vitest";

import type { ChainParams, Stats } from "@/api/types";

import fixture from "../../test/fixtures/api/v1/params.json";
import statsFixture from "../../test/fixtures/api/v1/stats.json";

import {
  aboutDuration,
  describeParam,
  formatDecimalPct,
  formatDuration,
  formatParam,
  formatUbze,
  groupOfModule,
  latestChange,
  PARAM_GROUPS,
  PARAMETERS,
  parameterGroups,
  paramAt,
  plainKey,
  rawText,
} from "./parameters";

const params = fixture as ChainParams;
const stats = statsFixture as Stats;
const rowsOf = (groups: ReturnType<typeof parameterGroups>, group: string) => groups.find((g) => g.group === group)!.rows;
const row = (groups: ReturnType<typeof parameterGroups>, chainName: string) =>
  groups.flatMap((g) => g.rows).find((r) => r.chainName === chainName)!;

describe("formatters", () => {
  it("reads durations in days, hours, minutes or seconds", () => {
    expect(formatDuration(1814400)).toBe("21 days");
    expect(formatDuration(86400)).toBe("1 day");
    expect(formatDuration(129600)).toBe("1.5 days");
    expect(formatDuration(59000)).toBe("16.4 hours");
    expect(formatDuration(1800)).toBe("30 minutes");
    expect(formatDuration(45)).toBe("45 seconds");
    expect(aboutDuration(59000)).toBe("about 16 hours");
    expect(formatParam("duration", "172800s")).toBe("2 days");
    expect(formatParam("duration", "soon")).toBeNull();
  });

  it("reads coins and base amounts in BZE", () => {
    expect(formatUbze({ denom: "ubze", amount: "100000000000" })).toBe("100,000 BZE");
    expect(formatUbze("1000")).toBe("0.001 BZE");
    expect(formatUbze([{ denom: "ubze", amount: "25000000000" }])).toBe("25,000 BZE");
    expect(formatUbze({ denom: "stake", amount: "50000000" })).toBe("50,000,000 stake");
    expect(formatUbze("abc")).toBeNull();
  });

  it("reads decimals as percentages", () => {
    expect(formatDecimalPct("0.334000000000000000")).toBe("33.4%");
    expect(formatDecimalPct("0.050000000000000000")).toBe("5%");
    expect(formatDecimalPct("0.000100000000000000")).toBe("0.01%");
    expect(formatDecimalPct("0.667")).toBe("66.7%");
    expect(formatDecimalPct(null)).toBeNull();
  });

  it("reads counts, gas, module accounts, lists and raw values", () => {
    expect(formatParam("count", "5543269")).toBe("5,543,269");
    expect(formatParam("gas", 25000)).toBe("25,000");
    expect(formatParam("address", "burner")).toBe("Burner module");
    expect(formatParam("address", "community_pool")).toBe("Community pool");
    expect(formatParam("address", "bze18mhtjwczlzqqgvw84uz8lrdv4hqule3jp8allp")).toBe("bze18mh…allp");
    expect(formatParam("list", [])).toBe("None right now");
    expect(formatParam("list", [{ channel_id: "channel-3", base_denom: "uusdc" }])).toBe("channel-3 · uusdc");
    expect(formatParam("list", "x")).toBeNull();
    expect(formatParam("raw", { denom: "ubze", amount: "1" })).toBe("1 ubze");
    expect(rawText([])).toBe("[]");
    expect(rawText(true)).toBe("true");
  });

  it("walks dotted paths and names unknown keys", () => {
    expect(paramAt({ a: { b: "1" } }, "a.b")).toBe("1");
    expect(paramAt({ a: "1" }, "a.b")).toBeUndefined();
    expect(paramAt(undefined, "a")).toBeUndefined();
    expect(plainKey("historical_entries")).toBe("Historical entries");
    expect(plainKey("createDenomRewardFee")).toBe("Create denom reward fee");
  });
});

describe("the dictionary", () => {
  const groups = parameterGroups(params, stats);

  it("has a group for every row and resolves every path against the recorded answer", () => {
    expect(new Set(PARAMETERS.map((d) => d.group))).toEqual(new Set(PARAM_GROUPS.map((g) => g.group)));
    const unanswered = groups.flatMap((g) => g.rows).filter((r) => r.value === null);
    expect(unanswered.map((r) => r.chainName)).toEqual([]);
  });

  it("renders the canvas's values", () => {
    const values = Object.fromEntries(groups.flatMap((g) => g.rows).filter((r) => r.def).map((r) => [r.chainName, r.value]));
    expect(values).toEqual({
      ValidatorMinGasFee: "0.01 ubze per gas",
      MaxBalanceIterations: "100 tokens",
      BlockedIbcInbound: "None right now",
      unbonding_time: "21 days",
      max_validators: "40",
      max_entries: "7 per validator",
      min_commission_rate: "5%",
      inflation: "9.8% per year",
      "inflation_min · inflation_max": "5% to 15%",
      goal_bonded: "20% of all BZE",
      inflation_rate_change: "10 points per year",
      community_tax: "5% of rewards",
      blocks_per_year: "5,543,269",
      signed_blocks_window: "10,000 blocks",
      min_signed_per_window: "5% of them",
      downtime_jail_duration: "10 minutes",
      slash_fraction_downtime: "0.01% of the stake",
      slash_fraction_double_sign: "5% of the stake",
      min_deposit: "100,000 BZE",
      max_deposit_period: "2 days",
      voting_period: "5 days",
      quorum: "33.4% of staked BZE",
      threshold: "more than 50% Yes",
      veto_threshold: "33.4% No with veto",
      "expedited_voting_period · expedited_threshold": "1 day · 66.7% Yes",
      createMarketFee: "25,000 BZE",
      marketMakerFee: "0.001 BZE per order",
      marketTakerFee: "0.1 BZE per order",
      "makerFeeDestination · takerFeeDestination": "Burner module",
      orderBookPerBlockMessages: "500",
      minNativeLiquidityForModuleSwap: "100,000 BZE",
      "orderBookExtraGasWindow · orderBookQueueExtraGas · fillOrdersExtraGas · orderBookQueueMessageScanExtraGas":
        "100 · 25,000 · 5,000 · 5,000",
      create_denom_fee: "25,000 BZE",
      createStakingRewardFee: "25,000 BZE",
      createTradingRewardFee: "50,000 BZE",
      extraGasForExitStake: "1,000,000",
      periodic_burning_weeks: "every 4 weeks",
      anon_article_limit: "5",
      anon_article_cost: "25,000 BZE",
      "publisher_respect_params.tax · publisher_respect_params.denom": "20%, in BZE",
    });
    expect(row(groups, "unbonding_time").raw).toBe("1814400s");
    expect(row(groups, "min_deposit").raw).toBe("100000000000 ubze");
    expect(row(groups, "publisher_respect_params.tax · publisher_respect_params.denom").raw).toBe("0.200000000000000000 · ubze");
  });

  it("interpolates live numbers into the explanations", () => {
    expect(row(groups, "max_validators").explain).toBe(
      "Only the 40 validators with the most stake produce blocks and earn rewards. 3 seats are taken right now.",
    );
    expect(row(groups, "goal_bonded").explain).toBe(
      "If less than 20% of BZE is staked, inflation rises to attract stakers. If more, it falls. 61.2% is staked today, so inflation is falling toward the minimum.",
    );
    expect(row(groups, "blocks_per_year").explain).toBe(
      "The block count the chain assumes when spreading rewards over the year, about 5.7 seconds per block. Blocks take 5.9 seconds right now.",
    );
    expect(row(groups, "signed_blocks_window").explain).toBe(
      "The chain looks at the last 10,000 blocks (about 16 hours) to judge whether a validator is online.",
    );
    expect(row(groups, "min_signed_per_window").explain).toBe(
      "A validator that misses more than 95% of the blocks in the window is jailed and stops earning.",
    );
    expect(row(groups, "unbonding_time").explain).toMatch(/^Unstaked BZE stays locked for 21 days before/);
    expect(row(groups, "makerFeeDestination · takerFeeDestination").explain).toMatch(/^Both fees are sent to the Burner/);
  });

  it("drops the sentences that need /stats when it is missing", () => {
    const bare = parameterGroups(params, null);
    expect(row(bare, "max_validators").explain).toBe("Only the 40 validators with the most stake produce blocks and earn rewards.");
    expect(row(bare, "goal_bonded").explain).toBe("If less than 20% of BZE is staked, inflation rises to attract stakers. If more, it falls.");
    expect(row(bare, "inflation").value).toBeNull();
  });

  it("says when inflation sits at a bound", () => {
    const atMin = { ...stats, inflation: "5.0", staked_share: "61" };
    expect(row(parameterGroups(params, atMin), "goal_bonded").explain).toMatch(/so inflation sits at the minimum\.$/);
    const atMax = { ...stats, inflation: "15.0", staked_share: "10" };
    expect(row(parameterGroups(params, atMax), "goal_bonded").explain).toMatch(/so inflation sits at the maximum\.$/);
    const rising = { ...stats, inflation: "9.0", staked_share: "10" };
    expect(row(parameterGroups(params, rising), "goal_bonded").explain).toMatch(/so inflation is rising toward the maximum\.$/);
  });

  it("appends the keys no row covers as advanced rows", () => {
    const staking = rowsOf(groups, "staking");
    expect(staking.filter((r) => r.advanced).map((r) => [r.name, r.chainName, r.value])).toEqual([
      ["Historical entries", "historical_entries", "10000"],
      ["Bond denom", "bond_denom", "ubze"],
    ]);
    const withUnknown: ChainParams = {
      ...params,
      burner: { params: { periodic_burning_weeks: "4", brand_new: { a: 1 } }, last_change: null },
    };
    const burner = rowsOf(parameterGroups(withUnknown, stats), "burner");
    expect(burner.map((r) => [r.chainName, r.value, r.advanced])).toEqual([
      ["periodic_burning_weeks", "every 4 weeks", false],
      ["brand_new", '{"a":1}', true],
    ]);
    // A row the dictionary flags as advanced covers its keys.
    expect(rowsOf(groups, "dex").filter((r) => r.advanced).map((r) => r.chainName)).toEqual([
      "orderBookExtraGasWindow · orderBookQueueExtraGas · fillOrdersExtraGas · orderBookQueueMessageScanExtraGas",
      "native_denom",
    ]);
  });

  it("renders a module the chain did not answer as not available", () => {
    const rest = Object.fromEntries(Object.entries(params).filter(([m]) => m !== "tradebin")) as ChainParams;
    const dex = rowsOf(parameterGroups(rest, stats), "dex");
    expect(dex).toHaveLength(7);
    expect(dex.every((r) => r.value === null && r.explain === "The chain did not answer for this value right now.")).toBe(true);
    const partial: ChainParams = { ...params, burner: { params: {}, last_change: null } };
    expect(rowsOf(parameterGroups(partial, stats), "burner")[0].value).toBeNull();
  });
});

describe("changes", () => {
  it("links each group to the proposal that last changed one of its modules", () => {
    const groups = parameterGroups(params, stats);
    expect(Object.fromEntries(groups.map((g) => [g.group, g.changedBy?.proposalId ?? null]))).toEqual({
      fees: null,
      staking: 11,
      inflation: null,
      penalties: null,
      gov: 40,
      dex: 44,
      factory: null,
      rewards: 44,
      burner: null,
      cointrunk: null,
    });
  });

  it("finds the latest change and the groups it touched", () => {
    expect(latestChange(params)).toEqual({
      proposalId: 44,
      time: "2025-11-24T09:00:00Z",
      height: 20250000,
      groups: ["DEX", "Rewards programs"],
    });
    expect(latestChange({ burner: { params: {}, last_change: null } })).toBeNull();
    expect(
      latestChange({ burner: { params: {}, last_change: { height: 9, time: "2026-01-01T00:00:00Z", proposal_id: null } } }),
    ).toEqual({ proposalId: null, time: "2026-01-01T00:00:00Z", height: 9, groups: ["Burner"] });
  });

  it("describes a proposal's changed key in plain words", () => {
    expect(groupOfModule("tradebin")).toBe("dex");
    expect(groupOfModule("nope")).toBeNull();
    expect(describeParam("tradebin", "marketTakerFee", { denom: "ubze", amount: "100000" })).toEqual({
      name: "Taker fee",
      value: "0.1 BZE per order",
    });
    expect(describeParam("staking", "unbonding_time", "1209600s")).toEqual({ name: "Unstaking period", value: "14 days" });
    expect(describeParam("mint", "inflation_min", "0.05")).toBeNull();
    expect(describeParam("staking", "bond_denom", "ubze")).toBeNull();
    expect(describeParam("staking", "unbonding_time", "soon")).toBeNull();
  });
});
