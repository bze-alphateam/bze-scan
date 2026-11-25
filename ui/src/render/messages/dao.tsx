import { Stack, Text } from "@chakra-ui/react";

import { Account, accountName, Fields, humanise, obj, str } from "./common";
import { renderer, type BodyProps, type MessageRenderer, type RenderMessage } from "./types";
import { eventsOf, listAttr, nestedMessages, NestedList, under } from "./v830-common";

// x/dao: DAOs with a member list or a staking electorate, their proposals and
// the admin's direct execution. A DAO reads as its name when the message
// carries one, else `DAO #id` (labels of DAO accounts follow with the backend).

const daoRef = (msg: RenderMessage) => {
  const id = str(msg, "dao_id");
  return id ? `DAO #${id}` : "a DAO";
};

const proposalRef = (msg: RenderMessage) => {
  const id = str(msg, "proposal_id");
  return id ? `DAO proposal #${id}` : "a DAO proposal";
};

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

/** An enum as the node prints it: the name (`YES`) or its number. */
const enumName = (value: unknown, names: string[]): string | null => {
  if (typeof value === "number" || (typeof value === "string" && /^\d+$/.test(value))) return names[Number(value)] ?? null;
  return typeof value === "string" && value !== "" ? value.toUpperCase().replace(/^(DAO_TYPE_|VOTE_OPTION_)/, "") : null;
};

const VOTE_WORDS: Record<string, string> = { YES: "Yes", NO: "No", ABSTAIN: "Abstain" };

function voteWord(msg: RenderMessage): string | null {
  const name = enumName(msg.body?.option, ["VOTE_OPTION_UNSPECIFIED", "YES", "NO", "ABSTAIN"]);
  return name ? (VOTE_WORDS[name] ?? humanise(name)) : null;
}

/** How a settled proposal ended, from its events; null when the message has none (pending or failed). */
export function settleOutcome(msg: RenderMessage): string | null {
  const executed = eventsOf(msg, "bze.dao.EventProposalExecuted")[0];
  if (executed) {
    const ok = executed.success === true || executed.success === "true";
    return ok ? "passed and executed" : "passed, but its actions failed";
  }
  const settled = eventsOf(msg, "bze.dao.EventProposalSettled")[0];
  const status = enumName(settled?.status, ["VOTING", "REJECTED", "EXECUTED", "EXECUTION_FAILED", "EXPIRED", "CANCELLED"]);
  switch (status) {
    case "REJECTED":
      return "rejected";
    case "EXPIRED":
      return "expired";
    case "EXECUTED":
      return "passed and executed";
    case "EXECUTION_FAILED":
      return "passed, but its actions failed";
    default:
      return null;
  }
}

const quoted = (name: string | null) => (name ? `“${name}”` : "");

function typeWords(msg: RenderMessage): string {
  const type = enumName(msg.body?.type, ["STATIC_LIST", "STAKE"]);
  return type === "STAKE" || type === "DAO_TYPE_STAKE" ? "staking vote" : "member list";
}

const createDao = renderer({
  category: "governance",
  title: (msg) => {
    const members = Array.isArray(msg.body?.members) ? msg.body.members.length : 0;
    const kind = typeWords(msg) === "member list" ? `member list, ${plural(members, "member")}` : "staking vote";
    return `Created DAO ${quoted(str(msg, "name"))} (${kind})`.replace("DAO  (", "DAO (");
  },
  reasonFor: () => "DAO creation fee",
  Body: function CreateDaoBody({ msg, ctx }: BodyProps) {
    const config = obj(msg.body?.config);
    return (
      <Fields
        rows={[
          { label: "Created by", value: <Account address={str(msg, "creator")} ctx={ctx} /> },
          { label: "Admin", value: <Account address={str(msg, "admin")} ctx={ctx} /> },
          { label: "Description", value: str(msg, "description") },
          { label: "Who votes", value: typeWords(msg) === "member list" ? "A fixed list of members" : "Stakers of the chosen tokens" },
          { label: "Voting period", value: typeof config?.voting_period === "string" ? config.voting_period : null },
        ]}
      />
    );
  },
});

const submitProposal = renderer({
  category: "governance",
  title: (msg) => `Proposed ${quoted(str(msg, "title")) || "a change"} in ${daoRef(msg)}`,
  reasonFor: () => "DAO proposal cost",
  Body: function SubmitProposalBody({ msg, ctx }: BodyProps) {
    const list = nestedMessages(msg);
    return (
      <Stack gap="4">
        <Fields
          rows={[
            { label: "Proposed by", value: <Account address={str(msg, "proposer")} ctx={ctx} /> },
            { label: "Summary", value: str(msg, "summary") },
            { label: "Expedited", value: msg.body?.expedited === true ? "Yes, it can end early once it clears the expedited bars" : null },
          ]}
        />
        {list.length > 0 && (
          <Stack gap="2" data-testid="what-it-would-do">
            <Text fontWeight="semibold">What it would do</Text>
            <NestedList list={list} ctx={ctx} />
          </Stack>
        )}
      </Stack>
    );
  },
});

const vote = renderer({
  category: "governance",
  title: (msg) => {
    const word = voteWord(msg);
    return `Voted ${word ? `${word} ` : ""}on ${proposalRef(msg)}`;
  },
  Body: ({ msg, ctx }: BodyProps) => (
    <Fields
      rows={[
        { label: "Voter", value: <Account address={str(msg, "voter")} ctx={ctx} /> },
        { label: "Vote", value: voteWord(msg) },
      ]}
    />
  ),
});

const settle = renderer({
  category: "governance",
  title: (msg) => {
    const outcome = settleOutcome(msg);
    return `Settled ${proposalRef(msg)}${outcome ? ` · ${outcome}` : ""}`;
  },
  reasonFor: () => "DAO proposal deposit",
  Body: function SettleBody({ msg, ctx }: BodyProps) {
    const settled = eventsOf(msg, "bze.dao.EventProposalSettled")[0];
    const executed = eventsOf(msg, "bze.dao.EventProposalExecuted")[0];
    const error = typeof executed?.error === "string" && executed.error !== "" ? executed.error : null;
    return (
      <Fields
        rows={[
          { label: "Settled by", value: <Account address={str(msg, "sender")} ctx={ctx} /> },
          { label: "Outcome", value: settleOutcome(msg) },
          { label: "Votes", value: settled ? `Yes ${settled.yes ?? 0} · No ${settled.no ?? 0} · Abstain ${settled.abstain ?? 0}` : null },
          { label: "Why the actions failed", value: error },
        ]}
      />
    );
  },
});

const cancel = renderer({
  category: "governance",
  title: (msg) => `Cancelled ${proposalRef(msg)}`,
  Body: ({ msg, ctx }: BodyProps) => <Fields rows={[{ label: "Cancelled by", value: <Account address={str(msg, "proposer")} ctx={ctx} /> }]} />,
});

const cleanup = renderer({
  category: "governance",
  title: (msg) => `Cleaned up ${proposalRef(msg)}`,
  Body: ({ msg, ctx }: BodyProps) => <Fields rows={[{ label: "Cleaned by", value: <Account address={str(msg, "sender")} ctx={ctx} /> }]} />,
});

const adminExecute = renderer({
  category: "governance",
  title: (msg) => `Ran ${plural(nestedMessages(msg).length, "action")} as ${daoRef(msg)}`,
  Body: function AdminExecuteBody({ msg, ctx }: BodyProps) {
    return (
      <Stack gap="4">
        <Fields rows={[{ label: "Run by", value: <Account address={str(msg, "admin")} ctx={ctx} /> }]} />
        <NestedList list={nestedMessages(msg, msg.events)} ctx={ctx} />
      </Stack>
    );
  },
});

const nominateAdmin = renderer({
  category: "governance",
  title: (msg, ctx) => `Nominated ${accountName(str(msg, "nominee"), ctx)} as admin of ${daoRef(msg)}`,
  counterparty: (msg) => (str(msg, "nominee") ? { kind: "account", address: str(msg, "nominee") as string } : null),
  Body: ({ msg, ctx }: BodyProps) => (
    <Fields
      rows={[
        { label: "Nominated by", value: <Account address={str(msg, "authority")} ctx={ctx} /> },
        { label: "Nominee", value: <Account address={str(msg, "nominee")} ctx={ctx} /> },
      ]}
    />
  ),
});

const acceptAdmin = renderer({
  category: "governance",
  title: (msg) => `Accepted the admin role of ${daoRef(msg)}`,
  Body: ({ msg, ctx }: BodyProps) => <Fields rows={[{ label: "New admin", value: <Account address={str(msg, "nominee")} ctx={ctx} /> }]} />,
});

const withdrawNomination = renderer({
  category: "governance",
  title: () => "Withdrew the admin nomination",
  Body: ({ msg, ctx }: BodyProps) => (
    <Fields
      rows={[
        { label: "DAO", value: daoRef(msg) },
        { label: "Withdrawn by", value: <Account address={str(msg, "authority")} ctx={ctx} /> },
      ]}
    />
  ),
});

/** The rule names of an `EventDaoConfigUpdated`, in words ("min_quorum" → "min quorum"). */
function changedRules(msg: RenderMessage): string[] {
  return eventsOf(msg, "bze.dao.EventDaoConfigUpdated").flatMap((a) => listAttr(a.fields)).map((f) => f.replace(/_/g, " "));
}

const updateConfig = renderer({
  category: "governance",
  title: (msg) => `Changed the rules of ${daoRef(msg)}`,
  Body: ({ msg, ctx }: BodyProps) => {
    const rules = changedRules(msg);
    return (
      <Fields
        rows={[
          { label: "Changed by", value: <Account address={str(msg, "authority")} ctx={ctx} /> },
          { label: "Rules changed", value: rules.length ? rules.join(", ") : null },
        ]}
      />
    );
  },
});

const updateMetadata = renderer({
  category: "governance",
  title: (msg) => {
    const name = str(msg, "name");
    const description = str(msg, "description");
    if (name && description) return `Renamed and re-described ${daoRef(msg)}`;
    return name ? `Renamed ${daoRef(msg)}` : `Re-described ${daoRef(msg)}`;
  },
  Body: ({ msg, ctx }: BodyProps) => (
    <Fields
      rows={[
        { label: "Changed by", value: <Account address={str(msg, "authority")} ctx={ctx} /> },
        { label: "New name", value: str(msg, "name") },
        { label: "New description", value: str(msg, "description") },
      ]}
    />
  ),
});

const updateElectorate = renderer({
  category: "governance",
  title: (msg) => `Changed the voters of ${daoRef(msg)}`,
  Body: function UpdateElectorateBody({ msg, ctx }: BodyProps) {
    const e = obj(msg.body?.electorate);
    const ids = Array.isArray(e?.sr_ids) ? e.sr_ids.map(String) : [];
    return (
      <Fields
        rows={[
          { label: "Changed by", value: <Account address={str(msg, "authority")} ctx={ctx} /> },
          { label: "Staking rewards counted", value: ids.length ? ids.map((i) => `#${i}`).join(", ") : null },
          { label: "Token rewards counted", value: typeof e?.dr_denom === "string" && e.dr_denom !== "" ? e.dr_denom : null },
        ]}
      />
    );
  },
});

const members = (msg: RenderMessage, key: "add" | "remove") => (Array.isArray(msg.body?.[key]) ? (msg.body?.[key] as unknown[]).map(String) : []);

const updateMembers = renderer({
  category: "governance",
  title: (msg) => {
    const added = members(msg, "add").length;
    const removed = members(msg, "remove").length;
    if (added && removed) return `Added ${added} and removed ${removed} members of ${daoRef(msg)}`;
    if (added) return `Added ${plural(added, "member")} to ${daoRef(msg)}`;
    if (removed) return `Removed ${plural(removed, "member")} from ${daoRef(msg)}`;
    return `Changed the members of ${daoRef(msg)}`;
  },
  Body: ({ msg, ctx }: BodyProps) => (
    <Fields
      rows={[
        { label: "Changed by", value: <Account address={str(msg, "authority")} ctx={ctx} /> },
        { label: "Added", value: members(msg, "add").length ? <Stack gap="1">{members(msg, "add").map((a) => <Account key={a} address={a} ctx={ctx} />)}</Stack> : null },
        { label: "Removed", value: members(msg, "remove").length ? <Stack gap="1">{members(msg, "remove").map((a) => <Account key={a} address={a} ctx={ctx} />)}</Stack> : null },
      ]}
    />
  ),
});

const updateParams = renderer({
  category: "governance",
  title: () => "Changed the settings of the DAO module",
  Body: function DaoParamsBody({ msg }: BodyProps) {
    const params = obj(msg.body?.params) ?? {};
    return (
      <Fields
        rows={Object.entries(params).map(([k, v]) => ({
          label: humanise(k),
          value: typeof v === "string" || typeof v === "number" ? String(v) : JSON.stringify(v),
        }))}
      />
    );
  },
});

/** The DAO module's messages by type URL; the proposal page reuses `/bze.dao.MsgUpdateParams`. */
export const daoRenderers: Record<string, MessageRenderer> = under("/bze.dao.", {
  MsgCreateDao: createDao,
  MsgSubmitProposal: submitProposal,
  MsgVote: vote,
  MsgSettleProposal: settle,
  MsgCancelProposal: cancel,
  MsgCleanupProposal: cleanup,
  MsgAdminExecute: adminExecute,
  MsgNominateAdmin: nominateAdmin,
  MsgAcceptAdminNomination: acceptAdmin,
  MsgWithdrawAdminNomination: withdrawNomination,
  MsgUpdateDaoConfig: updateConfig,
  MsgUpdateMetadata: updateMetadata,
  MsgUpdateElectorate: updateElectorate,
  MsgUpdateMembers: updateMembers,
  MsgUpdateParams: updateParams,
});

