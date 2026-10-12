import { Text } from "@chakra-ui/react";
import BigNumber from "bignumber.js";

import type { Coin } from "@/api/types";

import { Account, coinsText, Coins, Fields, parseCoins, str, transfersOf } from "./common";
import { symbolOf } from "./dex";
import { renderer, type BodyProps, type MessageRenderer, type RenderContext, type RenderMessage } from "./types";

/** What the signer paid into the module in this message (a ticket, a pot), from the transfers. */
function paid(msg: RenderMessage, ctx: RenderContext): Coin[] {
  const creator = str(msg, "creator");
  return transfersOf(msg, ctx)
    .filter((t) => t.sender === creator && t.kind === "transfer")
    .map((t) => ({ denom: t.denom, amount: t.amount }));
}

/** Where the signer's funds went: the transfer's recipient (the Burner module) as a chip. */
function Recipient({ msg, ctx }: BodyProps) {
  const creator = str(msg, "creator");
  const to = transfersOf(msg, ctx).find((t) => t.sender === creator && t.recipient)?.recipient ?? null;
  return to ? <Account address={to} ctx={ctx} /> : <Text as="span">Burner · module</Text>;
}

const fundBurner = renderer({
  category: "burner",
  title: (msg, ctx) => `Fund the Burner · ${coinsText(parseCoins(str(msg, "amount")), ctx)}`,
  counterparty: () => ({ kind: "name", name: "Burner" }),
  reasonFor: () => "set aside for burning",
  Body: function FundBurnerBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          {
            label: "From",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          { label: "To", value: <Recipient msg={msg} ctx={ctx} /> },
          {
            label: "Amount",
            value: <Coins list={parseCoins(str(msg, "amount"))} ctx={ctx} />,
          },
        ]}
      />
    );
  },
});

/** `10 BZE` of a raffle's denom from base units. */
function raffleAmount(msg: RenderMessage, key: string, ctx: RenderContext): string | null {
  const denom = str(msg, "denom");
  const amount = str(msg, key);
  return denom && amount ? coinsText([{ denom, amount }], ctx) : null;
}

const joinRaffle = renderer({
  category: "burner",
  title: (msg, ctx) => {
    const denom = str(msg, "denom");
    const ticket = paid(msg, ctx);
    const tickets = str(msg, "tickets");
    const detail = ticket.length
      ? ` · ticket ${coinsText(ticket, ctx)}`
      : tickets
        ? ` · ${tickets} ticket${tickets === "1" ? "" : "s"}`
        : "";
    return `Join the ${denom ? symbolOf(denom, ctx) : "unknown"} raffle${detail}`;
  },
  counterparty: () => ({ kind: "name", name: "Burner raffle" }),
  reasonFor: () => "raffle ticket",
  // The draw happens a couple of blocks later, so the transaction holds no prize.
  Body: function JoinRaffleBody({ msg, ctx }: BodyProps) {
    const tickets = str(msg, "tickets");
    return (
      <Fields
        rows={[
          {
            label: "Player",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          { label: "Tickets", value: tickets },
          { label: "Paid", value: <Coins list={paid(msg, ctx)} ctx={ctx} /> },
        ]}
      />
    );
  },
});

const startRaffle = renderer({
  category: "burner",
  title: (msg, ctx) => {
    const denom = str(msg, "denom");
    const pot = raffleAmount(msg, "pot", ctx);
    return `Start a raffle for ${denom ? symbolOf(denom, ctx) : "a token"}${pot ? ` · prize ${pot}` : ""}`;
  },
  counterparty: () => ({ kind: "name", name: "Burner raffle" }),
  reasonFor: () => "raffle prize pot",
  Body: function StartRaffleBody({ msg, ctx }: BodyProps) {
    const days = str(msg, "duration");
    const chances = str(msg, "chances");
    const ratio = str(msg, "ratio");
    const ratioPct = ratio != null ? new BigNumber(ratio).times(100) : null;
    // A ticket wins when a draw in 0–999,999 falls below `chances`.
    const chancePct = chances != null ? new BigNumber(chances).div(10_000) : null;
    return (
      <Fields
        rows={[
          {
            label: "Started by",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          { label: "Prize pot", value: raffleAmount(msg, "pot", ctx) },
          { label: "Ticket", value: raffleAmount(msg, "ticket_price", ctx) },
          {
            label: "Runs for",
            value: days ? `${days} day${days === "1" ? "" : "s"}` : null,
          },
          {
            label: "Winning chance",
            value: chancePct?.isFinite() ? `${chancePct.toFormat()}% per ticket` : null,
          },
          {
            label: "A winner gets",
            value: ratioPct?.isFinite() ? `${ratioPct.toFormat()}% of the pot` : null,
          },
        ]}
      />
    );
  },
});

const moveIbcLocked = renderer({
  category: "burner",
  title: () => "Move locked cross-chain coins to the Burner",
  counterparty: () => ({ kind: "name", name: "Burner" }),
  reasonFor: () => "moved to the Burner",
  Body: function MoveIbcLockedBody({ msg, ctx }: BodyProps) {
    const moved = transfersOf(msg, ctx).map((t) => ({
      denom: t.denom,
      amount: t.amount,
    }));
    return (
      <Fields
        rows={[
          {
            label: "Requested by",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          {
            label: "Token",
            value: str(msg, "denom") && symbolOf(str(msg, "denom")!, ctx),
          },
          {
            label: "Moved",
            value: moved.length ? <Coins list={moved} ctx={ctx} /> : null,
          },
        ]}
      />
    );
  },
});

const B = "/bze.burner.";

export const burnerRenderers: Record<string, MessageRenderer> = {
  [`${B}MsgFundBurner`]: fundBurner,
  [`${B}MsgJoinRaffle`]: joinRaffle,
  [`${B}MsgStartRaffle`]: startRaffle,
  [`${B}MsgMoveIbcLockedCoins`]: moveIbcLocked,
};
