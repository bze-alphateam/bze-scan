import { Badge, HStack, Image, Link, Stack, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { LuExternalLink } from "react-icons/lu";

import type { IbcTransfer } from "@/api/types";
import { TimeAgo } from "@/components/ui/time-ago";
import { shortHash } from "@/lib/format";

import { eventAttr, obj, str } from "./common";
import type { RenderContext, RenderMessage } from "./types";

// A cross-chain transfer's status, from the API's `ibc_transfers` (BZE's own
// records of the relayer's messages); the explorer never asks another chain.

/** The other chain's name: its registry name, else its chain id, else "the other chain". */
export function chainName(t: IbcTransfer): string {
  return t.counterparty.pretty_name ?? t.counterparty.chain_id ?? "the other chain";
}

const sameSeq = (a: unknown, b: number) => a != null && String(a) === String(b);

/**
 * The `ibc_transfers` row of one message: a `MsgTransfer` by its source
 * channel and the sequence its `send_packet` event names (the only row of the
 * channel when the event is missing); a relay message by the packet in its
 * body (BZE's side of the channel: the destination for a received packet,
 * the source for an acknowledgement or a timeout). Null when none matches.
 */
export function ibcRowFor(msg: RenderMessage, ctx: RenderContext): IbcTransfer | null {
  const rows = ctx.ibcTransfers;
  if (rows.length === 0) return null;
  if (msg.type_url === "/ibc.applications.transfer.v1.MsgTransfer") {
    const channel = str(msg, "source_channel");
    const seq = eventAttr(msg, "send_packet", "packet_sequence");
    const out = rows.filter((r) => r.direction === "out" && r.channel_id === channel);
    if (seq != null) return out.find((r) => sameSeq(seq, r.sequence)) ?? null;
    return out.length === 1 ? out[0] : null;
  }
  const p = obj(msg.body?.packet);
  if (!p) return null;
  const inbound = msg.type_url.endsWith("MsgRecvPacket");
  const channel = inbound ? p.destination_channel : p.source_channel;
  return rows.find((r) => r.direction === (inbound ? "in" : "out") && r.channel_id === channel && sameSeq(p.sequence, r.sequence)) ?? null;
}

interface Look {
  label: string;
  palette: "orange" | "green" | "red";
  /** The line under the chip. */
  detail: string | null;
}

function look(t: IbcTransfer): Look {
  const chain = chainName(t);
  switch (t.status) {
    case "pending":
      return t.expired
        ? {
            label: "Pending",
            palette: "orange",
            detail: "Expired, waiting for the refund",
          }
        : {
            label: "Pending",
            palette: "orange",
            detail: `waiting for ${chain} to confirm`,
          };
    case "delivered":
      return {
        label: "Delivered",
        palette: "green",
        detail: `received on ${chain}`,
      };
    case "refunded_rejected":
      return {
        label: "Refunded",
        palette: "red",
        detail: `rejected by ${chain}${t.ack_error ? `: ${t.ack_error}` : ""}`,
      };
    case "refunded_timeout":
      return {
        label: "Refunded",
        palette: "red",
        detail: "timed out; the tokens are back in your account",
      };
    case "received":
      return { label: "Received", palette: "green", detail: null };
    case "rejected":
      return {
        label: "Rejected",
        palette: "red",
        detail: t.ack_error ?? "rejected by BZE",
      };
  }
}

/** The other chain's explorer page of an address, from the registry's `${accountAddress}` template; null unless https. */
export function foreignAccountUrl(t: IbcTransfer): string | null {
  const template = t.counterparty.explorer_account_url;
  if (!template) return null;
  const address = t.direction === "out" ? t.receiver : t.sender;
  const url = template.replace("${accountAddress}", encodeURIComponent(address));
  return /^https:\/\//i.test(url) ? url : null;
}

/** The status chip, its explanation, the settling transaction and the other chain's explorer. */
export function IbcStatus({ t }: { t: IbcTransfer }) {
  const l = look(t);
  const foreign = foreignAccountUrl(t);
  const settled = t.result_tx_hash && t.result_tx_hash !== t.created_tx_hash ? t.result_tx_hash : null;
  return (
    <Stack gap="1.5" data-testid="ibc-status" data-status={t.status} data-expired={t.expired || undefined}>
      <HStack gap="2" wrap="wrap">
        <Badge colorPalette={l.palette} variant="subtle" rounded="full" size="md">
          {l.label}
        </Badge>
        {l.detail && (
          <Text as="span" fontSize="sm" color="fg.muted">
            {l.detail}
          </Text>
        )}
      </HStack>
      {settled && (
        <Text fontSize="sm" color="fg.muted">
          Settled by{" "}
          <Link asChild fontFamily="mono">
            <NextLink href={`/txs/${settled}`}>{shortHash(settled)}</NextLink>
          </Link>
          {t.result_time && (
            <>
              {", "}
              <TimeAgo time={t.result_time} />
            </>
          )}
        </Text>
      )}
      {foreign && (
        <Link href={foreign} target="_blank" rel="noopener noreferrer" fontSize="sm" alignSelf="start">
          See on {chainName(t)}&apos;s explorer <LuExternalLink aria-hidden />
        </Link>
      )}
    </Stack>
  );
}

/** The chain's logo next to its name, when the registry has one. */
export function ChainLogo({ t }: { t: IbcTransfer }) {
  const logo = t.counterparty.logo_url;
  if (!logo || !/^https:\/\//i.test(logo)) return null;
  return <Image src={logo} alt="" boxSize="5" rounded="full" flexShrink={0} />;
}
