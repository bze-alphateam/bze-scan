import { Link } from "@chakra-ui/react";
import BigNumber from "bignumber.js";
import NextLink from "next/link";

import { formatPct } from "@/lib/format";

import { Account, coins, coinsText, Coins, eventAttr, Fields, obj, str } from "./common";
import { renderer, type BodyProps, type MessageRenderer, type RenderMessage } from "./types";

const OPTIONS: Record<string, string> = {
  VOTE_OPTION_YES: "Yes",
  VOTE_OPTION_ABSTAIN: "Abstain",
  VOTE_OPTION_NO: "No",
  VOTE_OPTION_NO_WITH_VETO: "No with veto",
  "1": "Yes",
  "2": "Abstain",
  "3": "No",
  "4": "No with veto",
};

const optionName = (value: unknown) => OPTIONS[String(value)] ?? "an option";

/** The proposal a gov message is about: from the body, else from the `submit_proposal` event; null when neither has it. */
export function govProposalId(msg: RenderMessage): number | null {
  const id = Number(str(msg, "proposal_id") ?? eventAttr(msg, "submit_proposal", "proposal_id"));
  return Number.isSafeInteger(id) && id > 0 ? id : null;
}

/** `#14`; empty when the proposal is unknown. */
function proposalId(msg: RenderMessage): string {
  const id = govProposalId(msg);
  return id ? `#${id}` : "";
}

const onProposal = (msg: RenderMessage) => {
  const id = proposalId(msg);
  return id ? `proposal ${id}` : "a proposal";
};

/** A v1 proposal's title, or a v1beta1 legacy content's. */
function proposalTitle(msg: RenderMessage): string | null {
  const title = str(msg, "title") ?? obj(msg.body?.content)?.title;
  return typeof title === "string" && title !== "" ? title : null;
}

function ProposalLink({ msg }: { msg: RenderMessage }) {
  const id = proposalId(msg);
  if (!id) return null;
  return (
    <Link asChild>
      <NextLink href={`/proposals/${id.slice(1)}`}>{id}</NextLink>
    </Link>
  );
}

const submitProposal = renderer({
  category: "governance",
  title: (msg) => {
    const id = proposalId(msg);
    const title = proposalTitle(msg);
    return `Submit proposal${id ? ` ${id}` : ""}${title ? `: ${title}` : ""}`;
  },
  reasonFor: () => "proposal deposit",
  Body: function SubmitProposalBody({ msg, ctx }: BodyProps) {
    const deposit = coins(msg.body?.initial_deposit);
    return (
      <Fields
        rows={[
          { label: "Proposal", value: <ProposalLink msg={msg} /> },
          { label: "Title", value: proposalTitle(msg) },
          { label: "Proposer", value: <Account address={str(msg, "proposer")} ctx={ctx} /> },
          { label: "Deposit", value: deposit.length ? <Coins list={deposit} ctx={ctx} /> : null },
        ]}
      />
    );
  },
});

const deposit = renderer({
  category: "governance",
  title: (msg, ctx) => `Deposit ${coinsText(coins(msg.body?.amount), ctx)} on ${onProposal(msg)}`,
  reasonFor: () => "proposal deposit",
  Body: function DepositBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "Proposal", value: <ProposalLink msg={msg} /> },
          { label: "Depositor", value: <Account address={str(msg, "depositor")} ctx={ctx} /> },
          { label: "Amount", value: <Coins list={coins(msg.body?.amount)} ctx={ctx} /> },
        ]}
      />
    );
  },
});

const vote = renderer({
  category: "governance",
  title: (msg) => `Vote ${optionName(msg.body?.option)} on ${onProposal(msg)}`,
  Body: function VoteBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "Proposal", value: <ProposalLink msg={msg} /> },
          { label: "Voter", value: <Account address={str(msg, "voter")} ctx={ctx} /> },
          { label: "Vote", value: optionName(msg.body?.option) },
        ]}
      />
    );
  },
});

/** `Yes 70%, No 30%`. */
function weightedText(msg: RenderMessage): string {
  const raw = msg.body?.options;
  const options: unknown[] = Array.isArray(raw) ? raw : [];
  return options
    .map((o) => {
      const weight = new BigNumber(String(obj(o)?.weight ?? ""));
      const pct = weight.isFinite() ? ` ${formatPct(weight.times(100).toFixed()).replace(".0%", "%")}` : "";
      return `${optionName(obj(o)?.option)}${pct}`;
    })
    .join(", ");
}

const voteWeighted = renderer({
  category: "governance",
  title: (msg) => `Vote ${weightedText(msg) || "split"} on ${onProposal(msg)}`,
  Body: function VoteWeightedBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "Proposal", value: <ProposalLink msg={msg} /> },
          { label: "Voter", value: <Account address={str(msg, "voter")} ctx={ctx} /> },
          { label: "Vote", value: weightedText(msg) },
        ]}
      />
    );
  },
});

export const govRenderers: Record<string, MessageRenderer> = {
  "/cosmos.gov.v1.MsgSubmitProposal": submitProposal,
  "/cosmos.gov.v1beta1.MsgSubmitProposal": submitProposal,
  "/cosmos.gov.v1.MsgDeposit": deposit,
  "/cosmos.gov.v1beta1.MsgDeposit": deposit,
  "/cosmos.gov.v1.MsgVote": vote,
  "/cosmos.gov.v1beta1.MsgVote": vote,
  "/cosmos.gov.v1.MsgVoteWeighted": voteWeighted,
  "/cosmos.gov.v1beta1.MsgVoteWeighted": voteWeighted,
};
