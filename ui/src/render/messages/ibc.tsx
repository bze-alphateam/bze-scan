import { Text } from "@chakra-ui/react";

import type { Coin } from "@/api/types";
import { shortAddress } from "@/lib/format";

import { Account, coin, coinsText, Coins, eventAttr, Fields, ForeignAddress, obj, str, transfersOf } from "./common";
import { renderer, type BodyProps, type MessageRenderer, type RenderContext, type RenderMessage } from "./types";

const transfer = renderer({
  category: "cross_chain",
  title: (msg, ctx) => {
    const token = coin(msg, "token");
    const receiver = str(msg, "receiver");
    const channel = str(msg, "source_channel");
    return (
      `Cross-chain transfer · ${token ? coinsText([token], ctx) : "tokens"} → ${receiver ? shortAddress(receiver) : "another chain"}` +
      (channel ? ` through ${channel}` : "")
    );
  },
  reasonFor: () => "held for the cross-chain transfer",
  Body: function TransferBody({ msg, ctx }: BodyProps) {
    const token = coin(msg, "token");
    return (
      <Fields
        rows={[
          { label: "From", value: <Account address={str(msg, "sender")} ctx={ctx} /> },
          { label: "To", value: <ForeignAddress address={str(msg, "receiver")} /> },
          { label: "Amount", value: token && <Coins list={[token]} ctx={ctx} /> },
          { label: "Channel", value: str(msg, "source_channel") },
          { label: "Memo", value: str(msg, "memo") },
        ]}
      />
    );
  },
});

/** The packet's ICS-20 data as the receiving module's event reports it; null for another kind of packet. */
function received(msg: RenderMessage, ctx: RenderContext): { receiver: string | null; coins: Coin[]; sender: string | null } | null {
  const receiver = eventAttr(msg, "fungible_token_packet", "receiver");
  const amount = eventAttr(msg, "fungible_token_packet", "amount");
  if (!receiver && !amount) return null;
  // The voucher the receiver got, as the transfers know it (with its symbol), else the packet's own denom.
  const credited = transfersOf(msg, ctx).filter((t) => t.recipient === receiver && t.kind !== "burn");
  const list: Coin[] = credited.length
    ? credited.map((t) => ({ denom: t.denom, amount: t.amount }))
    : amount
      ? [{ denom: eventAttr(msg, "fungible_token_packet", "denom") ?? "", amount }]
      : [];
  return { receiver, coins: list, sender: eventAttr(msg, "fungible_token_packet", "sender") };
}

const packet = (msg: RenderMessage) => obj(msg.body?.packet);

function PacketFields({ msg, ctx }: BodyProps) {
  const p = packet(msg);
  return (
    <Fields
      rows={[
        { label: "Channel", value: typeof p?.destination_channel === "string" ? `${p.source_channel} → ${p.destination_channel}` : null },
        { label: "Sequence", value: p?.sequence != null ? String(p.sequence) : null },
        { label: "Relayed by", value: <Account address={str(msg, "signer")} ctx={ctx} /> },
      ]}
    />
  );
}

const recvPacket = renderer({
  category: "cross_chain",
  title: (msg, ctx) => {
    const r = received(msg, ctx);
    if (!r) return "Cross-chain packet received";
    return `Cross-chain transfer received: ${coinsText(r.coins, ctx)} to ${r.receiver ? shortAddress(r.receiver) : "an account"}`;
  },
  reasonFor: () => "received from another chain",
  Body: function RecvPacketBody({ msg, ctx }: BodyProps) {
    const r = received(msg, ctx);
    return (
      <>
        {r && (
          <Fields
            rows={[
              { label: "From", value: <ForeignAddress address={r.sender} /> },
              { label: "To", value: <Account address={r.receiver} ctx={ctx} /> },
              { label: "Amount", value: <Coins list={r.coins} ctx={ctx} /> },
            ]}
          />
        )}
        <PacketFields msg={msg} ctx={ctx} />
      </>
    );
  },
});

/** A relay message without funds of its own to explain: a one-line title and the relayer. */
function relay(title: string, reason?: string): MessageRenderer {
  return renderer({
    category: "cross_chain",
    title: () => title,
    reasonFor: reason ? () => reason : undefined,
    Body: PacketFields,
  });
}

function RelayerBody({ msg, ctx }: BodyProps) {
  const id = str(msg, "client_id") ?? str(msg, "connection_id") ?? str(msg, "channel_id");
  return (
    <Fields
      rows={[
        { label: "Relayed by", value: <Account address={str(msg, "signer")} ctx={ctx} /> },
        { label: "Identifier", value: id && <Text as="span" fontFamily="mono">{id}</Text> },
      ]}
    />
  );
}

/** A client, connection or channel message: one line and the relayer. */
function handshake(title: string): MessageRenderer {
  return renderer({ category: "cross_chain", title: () => title, Body: RelayerBody });
}

const CLIENT = "/ibc.core.client.v1.";
const CONNECTION = "/ibc.core.connection.v1.";
const CHANNEL = "/ibc.core.channel.v1.";

export const ibcRenderers: Record<string, MessageRenderer> = {
  "/ibc.applications.transfer.v1.MsgTransfer": transfer,
  [`${CHANNEL}MsgRecvPacket`]: recvPacket,
  [`${CHANNEL}MsgAcknowledgement`]: relay("Cross-chain delivery confirmed", "refunded"),
  [`${CHANNEL}MsgTimeout`]: relay("Cross-chain transfer timed out", "refunded after the timeout"),
  [`${CHANNEL}MsgTimeoutOnClose`]: relay("Cross-chain transfer timed out", "refunded after the timeout"),
  [`${CLIENT}MsgUpdateClient`]: handshake("Cross-chain client update"),
  [`${CLIENT}MsgCreateClient`]: handshake("Cross-chain client created"),
  [`${CLIENT}MsgUpgradeClient`]: handshake("Cross-chain client upgrade"),
  [`${CLIENT}MsgSubmitMisbehaviour`]: handshake("Cross-chain misbehaviour report"),
  [`${CLIENT}MsgRecoverClient`]: handshake("Cross-chain client recovery"),
  [`${CONNECTION}MsgConnectionOpenInit`]: handshake("Cross-chain connection setup"),
  [`${CONNECTION}MsgConnectionOpenTry`]: handshake("Cross-chain connection setup"),
  [`${CONNECTION}MsgConnectionOpenAck`]: handshake("Cross-chain connection setup"),
  [`${CONNECTION}MsgConnectionOpenConfirm`]: handshake("Cross-chain connection setup"),
  [`${CHANNEL}MsgChannelOpenInit`]: handshake("Cross-chain channel setup"),
  [`${CHANNEL}MsgChannelOpenTry`]: handshake("Cross-chain channel setup"),
  [`${CHANNEL}MsgChannelOpenAck`]: handshake("Cross-chain channel setup"),
  [`${CHANNEL}MsgChannelOpenConfirm`]: handshake("Cross-chain channel setup"),
  [`${CHANNEL}MsgChannelCloseInit`]: handshake("Cross-chain channel closing"),
  [`${CHANNEL}MsgChannelCloseConfirm`]: handshake("Cross-chain channel closing"),
};
