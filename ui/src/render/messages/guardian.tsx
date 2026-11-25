import { Stack, Text } from "@chakra-ui/react";
import type { ReactNode } from "react";

import { Account, accountName, Fields, humanise, obj, str } from "./common";
import { tokenName } from "./tokenfactory";
import { renderer, type BodyProps, type MessageRenderer, type RenderContext, type RenderMessage } from "./types";
import { EmergencyBadge, eventsOf, ParamsRows, under, wholePct } from "./v830-common";

// x/guardian: the emergency powers of the guardian key (every title starts
// with "Emergency:") and the governance-only messages that undo them. The
// renderers are exported through the registry; the emergency page reuses
// them for its history rows.

/** Who acted: the guardian's `signer`, or governance's `authority`. */
const actor = (msg: RenderMessage) => str(msg, "signer") ?? str(msg, "authority");

function EmergencyBody({ msg, ctx, rows }: BodyProps & { rows: { label: string; value: ReactNode }[] }) {
  const emergency = str(msg, "signer") !== null;
  return (
    <Stack gap="3">
      {emergency && <EmergencyBadge />}
      <Fields rows={[{ label: emergency ? "Done by" : "Decided by", value: <Account address={actor(msg)} ctx={ctx} /> }, ...rows]} />
    </Stack>
  );
}

const channel = (msg: RenderMessage) => str(msg, "channel_id") ?? "a channel";

/** The channel's other chain, from the transaction's cross-chain transfers when it carries one. */
function otherChain(channelId: string | null, ctx: RenderContext): string | null {
  const t = ctx.ibcTransfers.find((x) => x.channel_id === channelId);
  return t?.counterparty.pretty_name ?? t?.counterparty.chain_id ?? null;
}

const tightenRateLimit = renderer({
  category: "governance",
  title: (msg, ctx) => {
    const event = eventsOf(msg, "bze.guardian.v1.TightenRateLimitEvent")[0];
    const send = wholePct(str(msg, "max_percent_send") ?? (event?.new_send as string | undefined));
    const recv = wholePct(str(msg, "max_percent_recv") ?? (event?.new_recv as string | undefined));
    const limit = send && recv ? ` to ${send} send / ${recv} receive` : "";
    return `Emergency: lowered the IBC limit of ${tokenName(str(msg, "denom"), ctx)} on ${channel(msg)}${limit}`;
  },
  Body: function TightenBody({ msg, ctx }: BodyProps) {
    const event = eventsOf(msg, "bze.guardian.v1.TightenRateLimitEvent")[0];
    const change = (before: unknown, now: string | null) => {
      const old = wholePct(before as string | undefined);
      const next = wholePct(now);
      return next ? (old ? `${old} → ${next}` : next) : null;
    };
    return (
      <EmergencyBody
        msg={msg}
        ctx={ctx}
        rows={[
          { label: "Token", value: tokenName(str(msg, "denom"), ctx) },
          { label: "Channel", value: channel(msg) },
          { label: "Daily limit on sending", value: change(event?.old_send, str(msg, "max_percent_send")) },
          { label: "Daily limit on receiving", value: change(event?.old_recv, str(msg, "max_percent_recv")) },
        ]}
      />
    );
  },
});

const blockDenom = renderer({
  category: "governance",
  title: (msg, ctx) => `Emergency: blocked ${tokenName(str(msg, "denom"), ctx)} from crossing chains`,
  Body: ({ msg, ctx }: BodyProps) => <EmergencyBody msg={msg} ctx={ctx} rows={[{ label: "Token", value: tokenName(str(msg, "denom"), ctx) }]} />,
});

const unblockDenom = renderer({
  category: "governance",
  title: (msg, ctx) => `Unblocked ${tokenName(str(msg, "denom"), ctx)} for crossing chains`,
  Body: ({ msg, ctx }: BodyProps) => <EmergencyBody msg={msg} ctx={ctx} rows={[{ label: "Token", value: tokenName(str(msg, "denom"), ctx) }]} />,
});

const urls = (msg: RenderMessage): string[] => (Array.isArray(msg.body?.msg_type_urls) ? msg.body.msg_type_urls.map(String) : []);

const disableMessages = renderer({
  category: "governance",
  title: (msg) => {
    const n = urls(msg).length;
    return `Emergency: switched off ${n} ${n === 1 ? "kind" : "kinds"} of actions`;
  },
  Body: ({ msg, ctx }: BodyProps) => (
    <EmergencyBody
      msg={msg}
      ctx={ctx}
      rows={[
        {
          label: "Switched off",
          value: (
            <Stack gap="1" as="ul" listStyleType="none" m="0" p="0">
              {urls(msg).map((u) => (
                <Text as="li" key={u} title={u}>
                  {humanise(u)}
                </Text>
              ))}
            </Stack>
          ),
        },
      ]}
    />
  ),
});

const denoms = (msg: RenderMessage): string[] => (Array.isArray(msg.body?.denoms) ? msg.body.denoms.map(String) : []);

const haltTrading = renderer({
  category: "governance",
  title: (msg, ctx) => {
    const names = denoms(msg).map((d) => tokenName(d, ctx));
    return `Emergency: halted trading of ${names.length ? names.join(", ") : "tokens"}`;
  },
  Body: ({ msg, ctx }: BodyProps) => (
    <EmergencyBody msg={msg} ctx={ctx} rows={[{ label: "Tokens halted", value: denoms(msg).map((d) => tokenName(d, ctx)).join(", ") }]} />
  ),
});

const haltChain = renderer({
  category: "governance",
  title: (msg) => {
    const height = eventsOf(msg, "bze.guardian.v1.ChainHaltScheduledEvent")[0]?.height;
    return `Emergency: scheduled a chain halt${height != null && height !== "" ? ` at block ${String(height)}` : ""}`;
  },
  Body: ({ msg, ctx }: BodyProps) => {
    const event = eventsOf(msg, "bze.guardian.v1.ChainHaltScheduledEvent")[0];
    // The reason is free text from the guardian: shown as plain text, never as markup.
    return (
      <EmergencyBody
        msg={msg}
        ctx={ctx}
        rows={[
          { label: "Halt at block", value: event?.height != null ? String(event.height) : null },
          { label: "Reason", value: str(msg, "reason") },
        ]}
      />
    );
  },
});

const pauseAutomation = renderer({
  category: "governance",
  title: () => "Emergency: paused fee conversion and burning",
  Body: ({ msg, ctx }: BodyProps) => <EmergencyBody msg={msg} ctx={ctx} rows={[]} />,
});

const resumeAutomation = renderer({
  category: "governance",
  title: () => "Resumed fee conversion and burning",
  Body: ({ msg, ctx }: BodyProps) => <EmergencyBody msg={msg} ctx={ctx} rows={[]} />,
});

const closeChannel = renderer({
  category: "governance",
  title: (msg, ctx) => {
    const chain = otherChain(str(msg, "channel_id"), ctx);
    return `Emergency: closed ${channel(msg)}${chain ? ` to ${chain}` : ""}`;
  },
  Body: ({ msg, ctx }: BodyProps) => (
    <EmergencyBody
      msg={msg}
      ctx={ctx}
      rows={[
        { label: "Channel", value: channel(msg) },
        { label: "Other chain", value: otherChain(str(msg, "channel_id"), ctx) },
      ]}
    />
  ),
});

const reopenChannel = renderer({
  category: "governance",
  title: (msg) => `Reopened ${channel(msg)}`,
  Body: ({ msg, ctx }: BodyProps) => <EmergencyBody msg={msg} ctx={ctx} rows={[{ label: "Channel", value: channel(msg) }]} />,
});

/** The new key holder of a guardian `MsgUpdateParams`. */
const keyHolder = (msg: RenderMessage): string | null => {
  const params = msg.body?.params;
  const holder = typeof params === "object" && params !== null ? (params as Record<string, unknown>).guardian : null;
  return typeof holder === "string" && holder !== "" ? holder : null;
};

const updateParams = renderer({
  category: "governance",
  title: (msg, ctx) => {
    const holder = keyHolder(msg);
    return holder ? `Changed the emergency key holder to ${accountName(holder, ctx)}` : "Removed the emergency key holder";
  },
  Body: ({ msg, ctx }: BodyProps) => (
    <Stack gap="3">
      <Fields rows={[{ label: "Decided by", value: <Account address={str(msg, "authority")} ctx={ctx} /> }]} />
      <ParamsRows module="guardian" params={obj(msg.body?.params) ?? {}} ctx={ctx} />
    </Stack>
  ),
});

export const guardianRenderers: Record<string, MessageRenderer> = under("/bze.guardian.v1.", {
  MsgTightenRateLimit: tightenRateLimit,
  MsgBlockDenom: blockDenom,
  MsgUnblockDenom: unblockDenom,
  MsgDisableMessages: disableMessages,
  MsgHaltTradingDenoms: haltTrading,
  MsgHaltChain: haltChain,
  MsgPauseAutomation: pauseAutomation,
  MsgResumeAutomation: resumeAutomation,
  MsgCloseChannel: closeChannel,
  MsgReopenChannel: reopenChannel,
  MsgUpdateParams: updateParams,
});
