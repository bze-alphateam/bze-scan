import { Account, accountName, coins, coinsText, Coins, eventAttr, Fields, parseCoins, str, Validator, validatorName } from "./common";
import { renderer, validator, type BodyProps, type MessageRenderer, type RenderContext, type RenderMessage } from "./types";

/** What an event says was paid out; empty for nothing (an empty amount attribute). */
const paidOut = (msg: RenderMessage, event: string) => parseCoins(eventAttr(msg, event, "amount"));

const withText = (list: ReturnType<typeof coins>, ctx: RenderContext) => (list.length ? ` · ${coinsText(list, ctx)}` : "");

const withdrawReward = renderer({
  category: "staking",
  title: (msg, ctx) => `Claim staking rewards from ${validatorName(str(msg, "validator_address"), ctx)}`,
  counterparty: (msg) => validator(str(msg, "validator_address")),
  summary: (msg, ctx) =>
    `Claim staking rewards from ${validatorName(str(msg, "validator_address"), ctx)}${withText(paidOut(msg, "withdraw_rewards"), ctx)}`,
  reasonFor: () => "staking rewards",
  Body: function WithdrawRewardBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "Delegator", value: <Account address={str(msg, "delegator_address")} ctx={ctx} /> },
          { label: "Validator", value: <Validator operator={str(msg, "validator_address")} ctx={ctx} /> },
          { label: "Claimed", value: msg.events ? <Coins list={paidOut(msg, "withdraw_rewards")} ctx={ctx} /> : null },
        ]}
      />
    );
  },
});

const withdrawCommission = renderer({
  category: "staking",
  title: (msg, ctx) => `Claim the commission of ${validatorName(str(msg, "validator_address"), ctx)}`,
  reasonFor: () => "validator commission",
  Body: function WithdrawCommissionBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "Validator", value: <Validator operator={str(msg, "validator_address")} ctx={ctx} /> },
          { label: "Claimed", value: msg.events ? <Coins list={paidOut(msg, "withdraw_commission")} ctx={ctx} /> : null },
        ]}
      />
    );
  },
});

const setWithdrawAddress = renderer({
  category: "staking",
  title: (msg, ctx) => `Send future staking rewards to ${accountName(str(msg, "withdraw_address"), ctx)}`,
  Body: function SetWithdrawAddressBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "Delegator", value: <Account address={str(msg, "delegator_address")} ctx={ctx} /> },
          { label: "Rewards go to", value: <Account address={str(msg, "withdraw_address")} ctx={ctx} /> },
        ]}
      />
    );
  },
});

const fundCommunityPool = renderer({
  category: "send",
  title: (msg, ctx) => `Fund the community pool with ${coinsText(coins(msg.body?.amount), ctx)}`,
  counterparty: () => ({ kind: "name", name: "Community pool" }),
  reasonFor: () => "to the community pool",
  Body: function FundCommunityPoolBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "From", value: <Account address={str(msg, "depositor")} ctx={ctx} /> },
          { label: "Amount", value: <Coins list={coins(msg.body?.amount)} ctx={ctx} /> },
        ]}
      />
    );
  },
});

export const distributionRenderers: Record<string, MessageRenderer> = {
  "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward": withdrawReward,
  "/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission": withdrawCommission,
  "/cosmos.distribution.v1beta1.MsgSetWithdrawAddress": setWithdrawAddress,
  "/cosmos.distribution.v1beta1.MsgFundCommunityPool": fundCommunityPool,
};
