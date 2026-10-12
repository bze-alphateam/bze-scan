import type { ReactNode } from "react";

import type { Coin } from "@/api/types";

import { Account, coinsText, Coins, Fields, str, transfersOf } from "./common";
import { marketName, symbolOf } from "./dex";
import { renderer, type BodyProps, type MessageRenderer, type RenderContext, type RenderMessage } from "./types";

/** `10 HONEY` of `denomKey` from the body's base-unit `amountKey`; null when either is missing. */
function bodyCoin(msg: RenderMessage, amountKey: string, denomKey: string): Coin | null {
  const amount = str(msg, amountKey);
  const denom = str(msg, denomKey);
  return amount && denom ? { denom, amount } : null;
}

/** The coins the signer paid in (`out`) or received (`in`) in this message. */
function moved(msg: RenderMessage, ctx: RenderContext, way: "in" | "out"): Coin[] {
  const creator = str(msg, "creator");
  return transfersOf(msg, ctx)
    .filter((t) => (way === "in" ? t.recipient === creator : t.sender === creator))
    .map((t) => ({ denom: t.denom, amount: t.amount }));
}

const days = (n: string | null) => (n ? `${n} day${n === "1" ? "" : "s"}` : null);

/** "the HONEY staking reward" when the transfers name its denom, else "staking reward #7". */
function stakingRewardName(msg: RenderMessage, ctx: RenderContext, coins: Coin[]): string {
  return coins.length ? `the ${symbolOf(coins[0].denom, ctx)} staking reward` : `staking reward #${str(msg, "reward_id") ?? "?"}`;
}

/** "the HONEY denom reward". */
const denomRewardName = (msg: RenderMessage, ctx: RenderContext) => {
  const denom = str(msg, "denom");
  return denom ? `the ${symbolOf(denom, ctx)} denom reward` : "a denom reward";
};

/** The body every rewards message shares: who, which program, what moved. */
function rewardBody(rows: (msg: RenderMessage, ctx: RenderContext) => { label: string; value: ReactNode }[]) {
  return function RewardBody({ msg, ctx }: BodyProps) {
    const paid = moved(msg, ctx, "out");
    const got = moved(msg, ctx, "in");
    return (
      <Fields
        rows={[
          {
            label: "By",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          ...rows(msg, ctx),
          {
            label: "Paid in",
            value: paid.length ? <Coins list={paid} ctx={ctx} /> : null,
          },
          {
            label: "Received",
            value: got.length ? <Coins list={got} ctx={ctx} /> : null,
          },
        ]}
      />
    );
  };
}

const rewardId = (msg: RenderMessage) => ({
  label: "Reward",
  value: str(msg, "reward_id") ? `#${str(msg, "reward_id")}` : null,
});
const denomRow = (msg: RenderMessage, ctx: RenderContext) => ({
  label: "Token",
  value: str(msg, "denom") ? symbolOf(str(msg, "denom")!, ctx) : null,
});

const intoProgram = {
  counterparty: () => ({ kind: "name" as const, name: "Rewards" }),
};

const createStakingReward = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => {
    const staking = str(msg, "staking_denom");
    const prize = bodyCoin(msg, "prize_amount", "prize_denom");
    const d = str(msg, "duration");
    return (
      `Open a staking reward for ${staking ? symbolOf(staking, ctx) : "a token"}` +
      (prize ? ` · ${coinsText([prize], ctx)} per day` : "") +
      (d ? ` for ${days(d)}` : "")
    );
  },
  reasonFor: () => "prizes for the staking reward",
  Body: rewardBody((msg, ctx) => {
    const prize = bodyCoin(msg, "prize_amount", "prize_denom");
    const min = bodyCoin(msg, "min_stake", "staking_denom");
    return [
      {
        label: "Stake",
        value: str(msg, "staking_denom") ? symbolOf(str(msg, "staking_denom")!, ctx) : null,
      },
      { label: "Prize per day", value: prize ? coinsText([prize], ctx) : null },
      { label: "Runs for", value: days(str(msg, "duration")) },
      {
        label: "Minimum stake",
        value: min && min.amount !== "0" ? coinsText([min], ctx) : null,
      },
      { label: "Locked after leaving", value: days(str(msg, "lock")) },
    ];
  }),
});

const updateStakingReward = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg) =>
    `Extend staking reward #${str(msg, "reward_id") ?? "?"}${str(msg, "duration") ? ` by ${days(str(msg, "duration"))}` : ""}`,
  reasonFor: () => "prizes for the extra days",
  Body: rewardBody((msg) => [rewardId(msg), { label: "Extra days", value: str(msg, "duration") }]),
});

const joinStaking = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => {
    const paid = moved(msg, ctx, "out");
    // The amount's denom is the reward's, which only the transfers name.
    return paid.length
      ? `Lock ${coinsText(paid, ctx)} in ${stakingRewardName(msg, ctx, paid)}`
      : `Join staking reward #${str(msg, "reward_id") ?? "?"}`;
  },
  reasonFor: (t, msg) => (t.sender === str(msg, "creator") ? "locked in the staking reward" : "prizes claimed on joining"),
  Body: rewardBody((msg) => [rewardId(msg)]),
});

const exitStaking = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => `Leave ${stakingRewardName(msg, ctx, [])}`,
  reasonFor: () => "prizes claimed on leaving",
  Body: rewardBody((msg) => [rewardId(msg)]),
});

const claimStaking = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => {
    const got = moved(msg, ctx, "in");
    return `Claim staking-reward prizes${got.length ? ` · ${coinsText(got, ctx)}` : ""}`;
  },
  reasonFor: () => "staking-reward prize",
  Body: rewardBody((msg) => [rewardId(msg)]),
});

const distributeStaking = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => {
    const paid = moved(msg, ctx, "out");
    return `Add ${paid.length ? coinsText(paid, ctx) : "prizes"} to staking reward #${str(msg, "reward_id") ?? "?"}`;
  },
  reasonFor: () => "added to the staking reward's prizes",
  Body: rewardBody((msg) => [rewardId(msg)]),
});

const deleteStaking = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg) => `Close staking reward #${str(msg, "reward_id") ?? "?"}`,
  reasonFor: () => "returned from the closed staking reward",
  Body: rewardBody((msg) => [rewardId(msg)]),
});

const createTradingReward = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => {
    const prize = bodyCoin(msg, "prize_amount", "prize_denom");
    return `Open a trading reward on ${marketName(str(msg, "market_id"), ctx)}${prize ? ` · ${coinsText([prize], ctx)}` : ""}`;
  },
  reasonFor: () => "prizes for the trading reward",
  Body: rewardBody((msg, ctx) => {
    const prize = bodyCoin(msg, "prize_amount", "prize_denom");
    return [
      { label: "Market", value: marketName(str(msg, "market_id"), ctx) },
      { label: "Prize", value: prize ? coinsText([prize], ctx) : null },
      { label: "Runs for", value: days(str(msg, "duration")) },
      { label: "Winners", value: str(msg, "slots") },
    ];
  }),
});

const activateTradingReward = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg) => `Start trading reward #${str(msg, "reward_id") ?? "?"}`,
  Body: rewardBody((msg) => [rewardId(msg)]),
});

const createDenomReward = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => `Open ${denomRewardName(msg, ctx)}`,
  reasonFor: () => "denom reward creation fee",
  Body: rewardBody((msg, ctx) => [denomRow(msg, ctx)]),
});

const createSchedule = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => {
    const daily = bodyCoin(msg, "daily_amount", "prize_denom");
    const d = str(msg, "duration");
    return `Schedule ${daily ? `${coinsText([daily], ctx)} a day` : "prizes"} for ${denomRewardName(msg, ctx)}${d ? ` for ${days(d)}` : ""}`;
  },
  reasonFor: () => "prizes for the denom reward",
  Body: rewardBody((msg, ctx) => {
    const daily = bodyCoin(msg, "daily_amount", "prize_denom");
    return [
      denomRow(msg, ctx),
      { label: "Per day", value: daily ? coinsText([daily], ctx) : null },
      { label: "Runs for", value: days(str(msg, "duration")) },
    ];
  }),
});

const updateSchedule = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) =>
    `Extend schedule #${str(msg, "schedule_id") ?? "?"} of ${denomRewardName(msg, ctx)}${str(msg, "duration") ? ` by ${days(str(msg, "duration"))}` : ""}`,
  reasonFor: () => "prizes for the extra days",
  Body: rewardBody((msg, ctx) => [
    denomRow(msg, ctx),
    {
      label: "Schedule",
      value: str(msg, "schedule_id") && `#${str(msg, "schedule_id")}`,
    },
  ]),
});

const joinDenomReward = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => {
    const c = bodyCoin(msg, "amount", "denom");
    return `Lock ${c ? coinsText([c], ctx) : "tokens"} in ${denomRewardName(msg, ctx)}`;
  },
  reasonFor: (t, msg) => (t.sender === str(msg, "creator") ? "locked in the denom reward" : "prizes claimed on joining"),
  Body: rewardBody((msg, ctx) => [denomRow(msg, ctx)]),
});

const exitDenomReward = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => `Leave ${denomRewardName(msg, ctx)}`,
  reasonFor: () => "returned from the denom reward",
  Body: rewardBody((msg, ctx) => [denomRow(msg, ctx)]),
});

const claimDenomRewards = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => {
    const got = moved(msg, ctx, "in");
    return `Claim the prizes of ${denomRewardName(msg, ctx)}${got.length ? ` · ${coinsText(got, ctx)}` : ""}`;
  },
  reasonFor: () => "denom-reward prize",
  Body: rewardBody((msg, ctx) => [denomRow(msg, ctx)]),
});

const distributeDenomRewards = renderer({
  category: "rewards",
  ...intoProgram,
  title: (msg, ctx) => {
    const c = bodyCoin(msg, "amount", "prize_denom");
    return `Add ${c ? coinsText([c], ctx) : "prizes"} to ${denomRewardName(msg, ctx)}`;
  },
  reasonFor: () => "added to the denom reward's prizes",
  Body: rewardBody((msg, ctx) => [denomRow(msg, ctx)]),
});

const R = "/bze.rewards.";

export const rewardsRenderers: Record<string, MessageRenderer> = {
  [`${R}MsgCreateStakingReward`]: createStakingReward,
  [`${R}MsgUpdateStakingReward`]: updateStakingReward,
  [`${R}MsgJoinStaking`]: joinStaking,
  [`${R}MsgExitStaking`]: exitStaking,
  [`${R}MsgClaimStakingRewards`]: claimStaking,
  [`${R}MsgDistributeStakingRewards`]: distributeStaking,
  [`${R}MsgDeleteStakingReward`]: deleteStaking,
  [`${R}MsgCreateTradingReward`]: createTradingReward,
  [`${R}MsgActivateTradingReward`]: activateTradingReward,
  [`${R}MsgCreateDenomReward`]: createDenomReward,
  [`${R}MsgCreateDenomRewardSchedule`]: createSchedule,
  [`${R}MsgUpdateDenomRewardSchedule`]: updateSchedule,
  [`${R}MsgJoinDenomReward`]: joinDenomReward,
  [`${R}MsgExitDenomReward`]: exitDenomReward,
  [`${R}MsgClaimDenomRewards`]: claimDenomRewards,
  [`${R}MsgDistributeDenomRewards`]: distributeDenomRewards,
};
