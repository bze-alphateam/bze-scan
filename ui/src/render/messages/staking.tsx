import BigNumber from "bignumber.js";

import { formatDate, formatPct } from "@/lib/format";

import { Account, coin, coinText, Coins, eventAttr, Fields, FromTo, obj, str, Validator, validatorName } from "./common";
import { renderer, validator, type BodyProps, type MessageRenderer, type RenderContext, type RenderMessage } from "./types";

const amountText = (msg: RenderMessage, ctx: RenderContext) => {
  const c = coin(msg, "amount");
  return c ? coinText(c, ctx) : "an amount";
};

const amountRow = (msg: RenderMessage, ctx: RenderContext) => {
  const c = coin(msg, "amount");
  return c ? { label: "Amount", value: <Coins list={[c]} ctx={ctx} /> } : null;
};

/** A decimal rate (`0.050000000000000000`) as a percentage. */
const rate = (value: unknown) =>
  typeof value === "string" && new BigNumber(value).isFinite() ? formatPct(new BigNumber(value).times(100).toFixed()) : null;

function DelegationBody({ msg, ctx }: BodyProps) {
  return (
    <Fields
      rows={[
        { label: "Delegator", value: <Account address={str(msg, "delegator_address")} ctx={ctx} /> },
        { label: "Validator", value: <Validator operator={str(msg, "validator_address")} ctx={ctx} /> },
        amountRow(msg, ctx),
      ]}
    />
  );
}

const delegate = renderer({
  category: "staking",
  title: (msg, ctx) => `Stake ${amountText(msg, ctx)} with ${validatorName(str(msg, "validator_address"), ctx)}`,
  counterparty: (msg) => validator(str(msg, "validator_address")),
  reasonFor: () => "staked",
  Body: DelegationBody,
});

const completion = (msg: RenderMessage) => {
  const time = eventAttr(msg, "unbond", "completion_time");
  return time ? formatDate(time) : null;
};

const undelegate = renderer({
  category: "staking",
  title: (msg, ctx) => {
    const back = completion(msg);
    const base = `Unstake ${amountText(msg, ctx)} from ${validatorName(str(msg, "validator_address"), ctx)}`;
    return back ? `${base} · back on ${back}` : base;
  },
  counterparty: (msg) => validator(str(msg, "validator_address")),
  reasonFor: () => "unstaked, waiting out the unbonding period",
  Body: function UndelegateBody({ msg, ctx }: BodyProps) {
    return (
      <>
        <DelegationBody msg={msg} ctx={ctx} />
        <Fields rows={[{ label: "Back on", value: completion(msg) }]} />
      </>
    );
  },
});

const redelegate = renderer({
  category: "staking",
  title: (msg, ctx) =>
    `Move ${amountText(msg, ctx)} of stake from ${validatorName(str(msg, "validator_src_address"), ctx)} to ${validatorName(str(msg, "validator_dst_address"), ctx)}`,
  counterparty: (msg) => validator(str(msg, "validator_dst_address")),
  Body: function RedelegateBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "Delegator", value: <Account address={str(msg, "delegator_address")} ctx={ctx} /> },
          {
            label: "From → to",
            value: (
              <FromTo
                from={<Validator operator={str(msg, "validator_src_address")} ctx={ctx} />}
                to={<Validator operator={str(msg, "validator_dst_address")} ctx={ctx} />}
              />
            ),
          },
          amountRow(msg, ctx),
        ]}
      />
    );
  },
});

const cancelUnbonding = renderer({
  category: "staking",
  title: (msg, ctx) => `Cancel unstaking ${amountText(msg, ctx)} from ${validatorName(str(msg, "validator_address"), ctx)}`,
  counterparty: (msg) => validator(str(msg, "validator_address")),
  reasonFor: () => "staked again",
  Body: DelegationBody,
});

const moniker = (msg: RenderMessage) => {
  const m = obj(msg.body?.description)?.moniker;
  return typeof m === "string" && m !== "" && m !== "[do-not-modify]" ? m : null;
};

const createValidator = renderer({
  category: "staking",
  title: (msg, ctx) => `Create validator ${moniker(msg) ?? validatorName(str(msg, "validator_address"), ctx)}`,
  reasonFor: () => "self-stake",
  Body: function CreateValidatorBody({ msg, ctx }: BodyProps) {
    const value = coin(msg, "value");
    return (
      <Fields
        rows={[
          { label: "Validator", value: <Validator operator={str(msg, "validator_address")} ctx={ctx} /> },
          { label: "Name", value: moniker(msg) },
          { label: "Commission", value: rate(obj(msg.body?.commission)?.rate) },
          { label: "Self-stake", value: value && <Coins list={[value]} ctx={ctx} /> },
        ]}
      />
    );
  },
});

const editValidator = renderer({
  category: "staking",
  title: (msg, ctx) => `Edit validator ${moniker(msg) ?? validatorName(str(msg, "validator_address"), ctx)}`,
  Body: function EditValidatorBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "Validator", value: <Validator operator={str(msg, "validator_address")} ctx={ctx} /> },
          { label: "New name", value: moniker(msg) },
          { label: "New commission", value: rate(msg.body?.commission_rate) },
        ]}
      />
    );
  },
});

const unjail = renderer({
  category: "staking",
  title: (msg, ctx) => `Unjail ${validatorName(str(msg, "validator_addr"), ctx)}`,
  Body: function UnjailBody({ msg, ctx }: BodyProps) {
    return <Fields rows={[{ label: "Validator", value: <Validator operator={str(msg, "validator_addr")} ctx={ctx} /> }]} />;
  },
});

export const stakingRenderers: Record<string, MessageRenderer> = {
  "/cosmos.staking.v1beta1.MsgDelegate": delegate,
  "/cosmos.staking.v1beta1.MsgUndelegate": undelegate,
  "/cosmos.staking.v1beta1.MsgBeginRedelegate": redelegate,
  "/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation": cancelUnbonding,
  "/cosmos.staking.v1beta1.MsgCreateValidator": createValidator,
  "/cosmos.staking.v1beta1.MsgEditValidator": editValidator,
  "/cosmos.slashing.v1beta1.MsgUnjail": unjail,
};
