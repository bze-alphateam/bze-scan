import { Stack, Text } from "@chakra-ui/react";

import { Account, accountName, coins, coinsText, Coins, Fields, FromTo, obj, str } from "./common";
import { renderer, type BodyProps, type MessageRenderer } from "./types";

const send = renderer({
  category: "send",
  title: (msg, ctx) => `Send ${coinsText(coins(msg.body?.amount), ctx)} to ${accountName(str(msg, "to_address"), ctx)}`,
  summary: (msg, ctx) => `Send · ${coinsText(coins(msg.body?.amount), ctx)} → ${accountName(str(msg, "to_address"), ctx)}`,
  reasonFor: () => "sent",
  Body: function SendBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          {
            label: "From → to",
            value: <FromTo from={<Account address={str(msg, "from_address")} ctx={ctx} />} to={<Account address={str(msg, "to_address")} ctx={ctx} />} />,
          },
          { label: "Amount", value: <Coins list={coins(msg.body?.amount)} ctx={ctx} /> },
        ]}
      />
    );
  },
});

interface Side {
  address: string | null;
  coins: ReturnType<typeof coins>;
}

function sides(value: unknown): Side[] {
  return Array.isArray(value)
    ? value.map((v) => {
        const o = obj(v);
        return { address: typeof o?.address === "string" ? o.address : null, coins: coins(o?.coins) };
      })
    : [];
}

function SidesList({ list, ctx }: { list: Side[]; ctx: BodyProps["ctx"] }) {
  return (
    <Stack gap="2">
      {list.map((s, i) => (
        <Stack key={i} direction={{ base: "column", sm: "row" }} gap="2" align={{ sm: "center" }}>
          <Account address={s.address} ctx={ctx} />
          <Text as="span" fontWeight="semibold">
            {coinsText(s.coins, ctx)}
          </Text>
        </Stack>
      ))}
    </Stack>
  );
}

const multiSend = renderer({
  category: "send",
  title: (msg, ctx) => {
    const outputs = sides(msg.body?.outputs);
    if (outputs.length === 1) {
      return `Send ${coinsText(outputs[0].coins, ctx)} to ${accountName(outputs[0].address, ctx)}`;
    }
    return `Send to ${outputs.length} recipients`;
  },
  reasonFor: () => "sent",
  Body: function MultiSendBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "From", value: <SidesList list={sides(msg.body?.inputs)} ctx={ctx} /> },
          { label: "To", value: <SidesList list={sides(msg.body?.outputs)} ctx={ctx} /> },
        ]}
      />
    );
  },
});

export const bankRenderers: Record<string, MessageRenderer> = {
  "/cosmos.bank.v1beta1.MsgSend": send,
  "/cosmos.bank.v1beta1.MsgMultiSend": multiSend,
};
