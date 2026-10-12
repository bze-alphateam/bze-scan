import { describe, expect, it } from "vitest";

import type { ChainParams, Page, Proposal, ProposalSummary, ProposalVote, Validator, ValidatorList } from "@/api/types";

import params from "../../../test/fixtures/api/v1/params.json";
import p45Json from "../../../test/fixtures/api/v1/proposals/45.json";
import p48Json from "../../../test/fixtures/api/v1/proposals/48.json";
import votes48Json from "../../../test/fixtures/api/v1/proposals/48/votes.json";
import list from "../../../test/fixtures/api/v1/proposals.json";
import validatorsJson from "../../../test/fixtures/api/v1/validators.json";

import {
  checks,
  depositsReturned,
  describeProposal,
  endedText,
  endsIn,
  excerpt,
  govRules,
  kindLabel,
  metadataView,
  notVoted,
  resultLine,
  resultText,
  statusInfo,
  tallyShares,
  timeline,
  upgradeEta,
  verdict,
  votesSummary,
} from "./index";

const p48 = p48Json as Proposal;
const p45 = p45Json as Proposal;
const items = (list as Page<ProposalSummary>).items;
const votes = (votes48Json as Page<ProposalVote>).items;
const validators = (validatorsJson as ValidatorList).items as Validator[];
const chain = params as ChainParams;
const now = new Date("2099-10-11T10:00:00Z");
const GOV = "bze10d07y265gmmuvt4z0w9aw880jnsr700j8xlwyy";
const legacy = (content: Record<string, unknown>) => ({ "@type": "/cosmos.gov.v1.MsgExecLegacyContent", content, authority: GOV });

describe("what a proposal does", () => {
  it("reads a software upgrade and its cancellation", () => {
    expect(describeProposal(p48)).toEqual([{ type: "upgrade", name: "v8.2.0", height: 25200000 }]);
    const old = legacy({
      "@type": "/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal",
      title: "v5.1.2",
      plan: { name: "v5.1.2", height: "3646700" },
    });
    expect(describeProposal({ messages: [old] })).toEqual([{ type: "upgrade", name: "v5.1.2", height: 3646700 }]);
    expect(describeProposal({ messages: [{ "@type": "/cosmos.upgrade.v1beta1.MsgCancelUpgrade", authority: GOV }] })).toEqual([
      { type: "cancel_upgrade" },
    ]);
  });

  it("reads a community pool spend, new and legacy", () => {
    const spend = { type: "spend", recipient: "bze1jx4x3kn8mlz2s03zdpf2a38x9gl66llvjqdd55", amount: [{ denom: "ubze", amount: "3000000000000" }] };
    expect(describeProposal(p45)).toEqual([spend]);
    const old = legacy({
      "@type": "/cosmos.distribution.v1beta1.CommunityPoolSpendProposal",
      title: "Fund",
      description: "…",
      recipient: spend.recipient,
      amount: spend.amount,
    });
    expect(describeProposal({ messages: [old] })).toEqual([spend]);
  });

  it("reads a parameter change: the changed keys, or a module's whole set", () => {
    const old = legacy({
      "@type": "/cosmos.params.v1beta1.ParameterChangeProposal",
      title: "Max validators",
      changes: [
        { subspace: "staking", key: "MaxValidators", value: "40" },
        { subspace: "baseapp", key: "BlockParams", value: '{"max_bytes":"3145728"}' },
      ],
    });
    expect(describeProposal({ messages: [old] })).toEqual([
      {
        type: "params",
        module: "staking",
        whole: false,
        entries: [
          { key: "MaxValidators", value: "40" },
          { key: "baseapp/BlockParams", value: '{"max_bytes":"3145728"}' },
        ],
      },
    ]);
    const update = { "@type": "/bze.tradebin.v1.MsgUpdateParams", authority: GOV, params: { maker_fee: "1000ubze", market_fee: "25000000000ubze" } };
    expect(describeProposal({ messages: [update] })).toEqual([
      {
        type: "params",
        module: "tradebin",
        whole: true,
        entries: [
          { key: "maker_fee", value: "1000ubze" },
          { key: "market_fee", value: "25000000000ubze" },
        ],
      },
    ]);
    expect(describeProposal({ messages: [{ "@type": "/cosmos.staking.v1beta1.MsgUpdateParams", params: {} }] })[0]).toMatchObject({
      module: "staking",
    });
  });

  it("reads CoinTrunk publishers and domains, added or removed", () => {
    const publisher = legacy({
      "@type": "/bze.cointrunk.v1.PublisherProposal",
      name: "BZEAlphaTeam",
      address: "bze1ucx0c63jut4x9ekstszed64hmcumaa62k84xex",
      active: true,
    });
    const removed = { "@type": "/bze.cointrunk.MsgAcceptDomain", authority: GOV, domain: "twitter.com", active: false };
    expect(describeProposal({ messages: [publisher, removed] })).toEqual([
      { type: "publisher", add: true, name: "BZEAlphaTeam", address: "bze1ucx0c63jut4x9ekstszed64hmcumaa62k84xex" },
      { type: "domain", add: false, domain: "twitter.com" },
    ]);
  });

  it("reads a cross-chain repair", () => {
    const old = legacy({
      "@type": "/ibc.core.client.v1.ClientUpdateProposal",
      title: "Update IBC Client to dungeon-1",
      subject_client_id: "07-tendermint-34",
      substitute_client_id: "07-tendermint-37",
    });
    expect(describeProposal({ messages: [old] })).toEqual([{ type: "ibc_client", subject: "07-tendermint-34", substitute: "07-tendermint-37" }]);
  });

  it("leaves a text proposal to its summary", () => {
    expect(describeProposal({ messages: [legacy({ "@type": "/cosmos.gov.v1beta1.TextProposal", title: "400M", description: "…" })] })).toEqual([
      { type: "text" },
    ]);
    expect(describeProposal({ messages: [] })).toEqual([{ type: "text" }]);
    expect(describeProposal({ messages: null })).toEqual([{ type: "text" }]);
  });

  it("tables any other message by its fields, titled by its type", () => {
    const send = { "@type": "/cosmos.bank.v1beta1.MsgSend", from_address: "bze1a", to_address: "bze1b", amount: [{ denom: "ubze", amount: "1" }] };
    expect(describeProposal({ messages: [send] })).toEqual([
      {
        type: "other",
        title: "Send",
        typeUrl: "/cosmos.bank.v1beta1.MsgSend",
        entries: [
          { key: "from_address", value: "bze1a" },
          { key: "to_address", value: "bze1b" },
          { key: "amount", value: [{ denom: "ubze", amount: "1" }] },
        ],
      },
    ]);
  });

  it("labels every kind", () => {
    expect(
      ["software_upgrade", "community_pool_spend", "parameter_change", "cointrunk_publisher", "ibc_client_update", "text", "other", "new"].map(
        kindLabel,
      ),
    ).toEqual([
      "Software upgrade",
      "Community pool spend",
      "Parameter change",
      "CoinTrunk publisher",
      "Cross-chain repair",
      "Text",
      "Other",
      "Other",
    ]);
  });
});

describe("the votes", () => {
  const rules = govRules(chain, false)!;

  it("reads the chain's thresholds, the expedited ones for an expedited proposal", () => {
    expect(rules).toEqual({ quorum: "0.334000000000000000", threshold: "0.500000000000000000", vetoThreshold: "0.334000000000000000" });
    expect(govRules(chain, true)?.threshold).toBe("0.667000000000000000");
    expect(govRules({}, false)).toBeNull();
    expect(govRules(undefined, false)).toBeNull();
  });

  it("shares each option of the votes cast", () => {
    expect(tallyShares(p48.tally).map((s) => [s.label, s.pct?.slice(0, 5)])).toEqual([
      ["Yes", "89.55"],
      ["No", "5.970"],
      ["Abstain", "2.985"],
      ["Veto", "1.492"],
    ]);
    expect(tallyShares(null).every((s) => s.pct === null)).toBe(true);
    expect(resultText(p48.tally)).toBe("Yes 89.6%");
    expect(resultText(items[3].tally)).toBe("No 70.6%");
    expect(resultText(null)).toBeNull();
  });

  it("passes the three checks when enough voted, enough said yes and nobody vetoed", () => {
    const c = checks(p48, rules)!;
    expect(c.turnout).toMatchObject({ ok: true, value: "82.71605", need: "33.4" });
    expect(c.yes.ok).toBe(true);
    expect(c.veto.ok).toBe(true);
    expect(verdict(c)).toEqual({ pass: true, text: "On track to pass" });
  });

  it("fails on a quorum not reached, a veto, or too few yes votes", () => {
    expect(verdict(checks({ ...p48, turnout_pct: "20.00000" }, rules)!).text).toBe("Would fail: not enough people voted yet");
    const vetoed = { yes: "50", no: "0", abstain: "0", no_with_veto: "50" };
    expect(verdict(checks({ tally: vetoed, turnout_pct: "90" }, rules)!).text).toBe("Would fail: vetoed");
    const split = { yes: "50", no: "50", abstain: "900", no_with_veto: "0" };
    const c = checks({ tally: split, turnout_pct: "90" }, rules)!;
    expect(c.yes).toMatchObject({ ok: false, value: "50" });
    expect(verdict(c).text).toBe("Would fail: not enough Yes votes");
    expect(checks({ tally: null, turnout_pct: null }, rules)).toBeNull();
    expect(checks(p48, null)).toBeNull();
  });

  it("lists the bonded validators that have not voted, and sums up who did", () => {
    const validatorVotes = votes.filter((v) => v.validator_operator);
    expect(notVoted(validators, validatorVotes).map((v) => v.moniker)).toEqual(["Deep Fields"]);
    expect(votesSummary(validatorVotes, 1, 5)).toBe("2 validators voted Yes, 1 did not vote. 3 accounts voted on their own.");
    expect(votesSummary([{ option: "yes" }, { option: "no" }, { option: "yes" }], 0, 4)).toBe(
      "2 validators voted Yes, 1 voted No. 1 account voted on its own.",
    );
    expect(votesSummary([], 3, 0)).toBe("3 did not vote. 0 accounts voted on their own.");
    expect(votesSummary([], 0, 2)).toBe("No validator voted. 2 accounts voted on their own.");
  });
});

describe("dates and status", () => {
  it("names the status, a deposit period that ended as dropped", () => {
    expect(items.map((p) => statusInfo(p, now).text)).toEqual(["Deposit period", "Voting now", "Passed", "Rejected", "Passed", "Passed"]);
    expect(statusInfo(items[0], new Date("2099-10-13T00:00:00Z")).text).toBe("Dropped");
    expect(statusInfo({ status: "dropped", deposit_end_time: "2099-10-01T00:00:00Z" }, now)).toEqual({ text: "Dropped", tone: "gray" });
    expect(endedText({ status: "dropped", voting_end_time: null }, now)).toBeNull();
  });

  it("counts down to the end of voting", () => {
    expect(endsIn(p48.voting_end_time!, now)).toBe("in 3 days 4 h");
    expect(endsIn("2099-10-11T15:30:00Z", now)).toBe("in 5 h");
    expect(endsIn("2099-10-11T10:12:30Z", now)).toBe("in 12 min");
    expect(endsIn("2099-10-11T09:00:00Z", now)).toBe("ended");
    expect(items.map((p) => endedText(p, now))).toEqual([null, "ends in 4 days", "Jul 21, 2026", "May 7, 2026", "Dec 13, 2025", "May 6, 2025"]);
  });

  it("words the result line", () => {
    expect(resultLine(p45, now)).toBe("Passed Dec 13, 2025 · 100% Yes · 70.4% turnout");
  });

  it("estimates the upgrade's date from the blocks left and the block time", () => {
    // 79,550 blocks of 5.9 s: 5 days 10 h 22 min 25 s.
    expect(upgradeEta(25200000, 25120450, 5900, now)?.toISOString()).toBe("2099-10-16T20:22:25.000Z");
    expect(upgradeEta(25100000, 25120450, 5900, now)).toBeNull();
    expect(upgradeEta(25200000, null, 5900, now)).toBeNull();
    expect(upgradeEta(25200000, 25120450, null, now)).toBeNull();
  });

  it("orders the timeline", () => {
    const eta = new Date("2099-10-16T20:22:25Z");
    expect(timeline(p48, "100,000 BZE", { height: 25200000, eta, reached: false }, now).map((i) => [i.label, i.done])).toEqual([
      ["Submitted", true],
      ["Deposit of 100,000 BZE reached, voting opened", true],
      ["Voting ends", false],
      ["Upgrade happens", false],
    ]);
    expect(timeline(p45, null, null, now).map((i) => [i.label, i.height])).toEqual([
      ["Submitted", null],
      ["Deposit reached, voting opened", null],
      ["Voting ended", null],
      ["Passed and executed", 21512345],
    ]);
    expect(timeline({ ...p48, status: "passed" }, null, { height: 25200000, eta: null, reached: true }, now).at(-1)).toMatchObject({
      label: "Upgrade happened",
      done: true,
    });
  });
});

describe("the proposer's text", () => {
  it("cuts the summary at a word", () => {
    expect(excerpt("short", 240)).toBe("short");
    expect(excerpt("one two three four five", 12)).toBe("one two…");
    expect(excerpt(null, 10)).toBe("");
  });

  it("links a URL metadata and keeps text as text", () => {
    expect(metadataView(p48.metadata)).toEqual({ href: "https://github.com/bze-alphateam/bze/releases/tag/v8.2.0" });
    expect(metadataView("ipfs://bafy")).toEqual({ href: "https://ipfs.io/ipfs/bafy" });
    expect(metadataView("see the forum")).toEqual({ text: "see the forum" });
    expect(metadataView("")).toBeNull();
  });

  it("returns the deposits unless vetoed", () => {
    expect(depositsReturned("passed", false)).toBe(true);
    expect(depositsReturned("rejected", false)).toBe(true);
    expect(depositsReturned("rejected", true)).toBe(false);
    expect(depositsReturned("voting_period", false)).toBe(false);
    expect(depositsReturned("dropped", false)).toBe(false);
  });
});
