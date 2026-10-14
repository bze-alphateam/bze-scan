import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { ChainParams, Page, Proposal, ProposalDeposit, ProposalNonVoter, ProposalVote, Stats, Status, TokenSummary, ValidatorList } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import params from "../../../test/fixtures/api/v1/params.json";
import p43Json from "../../../test/fixtures/api/v1/proposals/43.json";
import deposits43 from "../../../test/fixtures/api/v1/proposals/43/deposits.json";
import votes43 from "../../../test/fixtures/api/v1/proposals/43/votes.json";
import p45Json from "../../../test/fixtures/api/v1/proposals/45.json";
import deposits45 from "../../../test/fixtures/api/v1/proposals/45/deposits.json";
import nonVoters45 from "../../../test/fixtures/api/v1/proposals/45/non-voters.json";
import votes45 from "../../../test/fixtures/api/v1/proposals/45/votes.json";
import p48Json from "../../../test/fixtures/api/v1/proposals/48.json";
import deposits48 from "../../../test/fixtures/api/v1/proposals/48/deposits.json";
import votes48 from "../../../test/fixtures/api/v1/proposals/48/votes.json";
import stats from "../../../test/fixtures/api/v1/stats.json";
import status from "../../../test/fixtures/api/v1/status.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";
import validators from "../../../test/fixtures/api/v1/validators.json";

import { ProposalPage } from "./proposal-page";

const p48 = p48Json as Proposal;
const p45 = p45Json as Proposal;
// 43 was tallied before the explorer ran: the snapshots command read its turnout, votes and deposit from the archive.
const p43 = p43Json as Proposal;
// 45 again, rejected by a No majority.
const rejected: Proposal = { ...p45, status: "rejected", tally: { yes: "0", no: p45.tally!.yes, abstain: "0", no_with_veto: "0" } };
const VOTES: Record<number, Page<ProposalVote>> = {
  48: votes48 as Page<ProposalVote>,
  45: votes45 as Page<ProposalVote>,
  43: votes43 as Page<ProposalVote>,
};
const DEPOSITS: Record<number, Page<ProposalDeposit>> = {
  48: deposits48 as Page<ProposalDeposit>,
  45: deposits45 as Page<ProposalDeposit>,
  43: deposits43 as Page<ProposalDeposit>,
};

// The fake API filters like the backend: by option, and by validator owners.
const proposalVotes: ExplorerApi["proposalVotes"] = (id, p) =>
  Promise.resolve({
    items: VOTES[id].items.filter(
      (v) =>
        (p.voter !== "validators" || v.validator_operator) &&
        (!p.option || (p.option === "weighted" ? v.option === null : v.option === p.option)),
    ),
    next_cursor: null,
  });

function api(p: Proposal = p48, extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    proposal: () => Promise.resolve(p),
    proposalVotes,
    proposalDeposits: (id) => Promise.resolve(DEPOSITS[id]),
    proposalNonVoters: () => Promise.resolve(nonVoters45 as Page<ProposalNonVoter>),
    params: () => Promise.resolve(params as ChainParams),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    validators: () => Promise.resolve(validators as ValidatorList),
    stats: () => Promise.resolve(stats as Stats),
    status: () => Promise.resolve(status as Status),
    ...extra,
  });
}

const tiles = async () => {
  const summary = await screen.findByTestId("summary-tiles");
  return () => within(summary).getAllByTestId("tile").map((t) => t.textContent);
};

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"], now: new Date("2099-10-11T10:00:00Z") });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ProposalPage", () => {
  it("renders the proposer's text as Markdown, folded when long, with no HTML", async () => {
    const summary = `## Why\n\nThis **halts** USDC.n. <script>window.pwned = true</script>\n\n${"More words. ".repeat(80)}`;
    renderWithProviders(<ProposalPage id={48} />, { api: api({ ...p48, summary }) });
    const from = await screen.findByTestId("from-proposer");
    expect(within(from).getByRole("heading", { name: "Why" })).toBeInTheDocument();
    expect(within(from).getByText("halts").tagName).toBe("STRONG");
    expect(from.querySelector("script")).toBeNull();
    expect((window as { pwned?: boolean }).pwned).toBeUndefined();

    const text = within(from).getByTestId("proposer-text");
    expect(text).toHaveAttribute("data-folded");
    await userEvent.click(within(from).getByRole("button", { name: "Read the full text" }));
    expect(text).not.toHaveAttribute("data-folded");
    expect(within(from).getByRole("button", { name: "Show less" })).toHaveAttribute("aria-expanded", "true");
  });

  it("shows the header, the card, four tally bars and the who-voted rows as skeletons on the first load", () => {
    renderWithProviders(<ProposalPage id={48} />, { api: pendingApi() });
    expect(screen.getByTestId("proposal-skeleton")).toBeInTheDocument();
    expect(screen.getByTestId("card-skeleton")).toBeInTheDocument();
    expect(screen.getAllByTestId("tally-skeleton")).toHaveLength(4);
    expect(screen.getAllByTestId("list-skeleton-row")).toHaveLength(5);
  });

  it("renders a proposal being voted on: header, upgrade, checks and where to vote", async () => {
    renderWithProviders(<ProposalPage id={48} />, { api: api() });
    expect(await screen.findByRole("heading", { level: 1, name: "Upgrade network to v8.2.0" })).toBeInTheDocument();
    expect(screen.getByTestId("proposal-status")).toHaveTextContent(/^Voting$/);
    expect(screen.getByTestId("proposal-status")).toHaveAttribute("data-tag", "accent");
    // Yes is counted without Abstain: 300M of 325M.
    const texts = await tiles();
    await waitFor(() =>
      expect(texts()).toEqual([
        "If it ended nowPassesall three checks met",
        "Yes so far92.3%300M BZE · more than 50% needed",
        "Turnout so far82.7%33.4% needed · reached",
        "Voting endsin 3 days 4 hOct 14, 2099, 14:00 UTC",
        "Proposed byHOHoneycombSubmitted Oct 9, 2099, 10:00 UTC",
      ]),
    );
    expect(screen.queryByTestId("historical-turnout")).not.toBeInTheDocument();

    const what = screen.getByTestId("what-it-does");
    // 79,550 blocks left at 5.9 s a block.
    await waitFor(() =>
      expect(within(what).getByTestId("action")).toHaveTextContent("Upgrades the chain software to v8.2.0 at block 25,200,000, expected around Oct 16, 2099."),
    );
    expect(within(what).getByRole("link", { name: /Release notes on GitHub/ })).toHaveAttribute(
      "href",
      "https://github.com/bze-alphateam/bze/releases/tag/v8.2.0",
    );
    expect(within(what).getByRole("link", { name: "25,200,000" })).toHaveAttribute("href", "/blocks/25200000");
    expect(within(what).getByTestId("from-proposer")).toHaveTextContent("It adds halted denoms on the DEX");

    const votes = screen.getByTestId("votes-card");
    expect(votes).toHaveTextContent("Votes so far");
    expect(votes).toHaveTextContent("refreshed every minute");
    expect(await within(votes).findByTestId("verdict")).toHaveTextContent("On track to pass");
    expect(within(votes).getAllByTestId("check").map((c) => c.getAttribute("data-ok"))).toEqual(["true", "true", "true"]);
    expect(votes).toHaveTextContent("82.7% of staked BZE voted · 33.4% needed");
    expect(await within(votes).findByText("300M BZE")).toBeInTheDocument();
    expect(votes).toHaveTextContent("5 accounts have voted so far.");

    const vote = screen.getByTestId("want-to-vote");
    expect(within(vote).getByRole("link", { name: /Vote on Ping.pub with Keplr or Leap/ })).toHaveAttribute(
      "href",
      expect.stringMatching(/\/gov\/48$/),
    );
    expect(screen.getAllByTestId("timeline-item").map((i) => i.firstChild?.nextSibling?.firstChild?.textContent)).toEqual([
      "Submitted",
      "Deposit of 100,000 BZE reached, voting opened",
      "Voting ends",
      "Upgrade happens",
    ]);
  });

  it("lists the validators' votes, the ones that have not voted, and everyone's votes", async () => {
    renderWithProviders(<ProposalPage id={48} />, { api: api() });
    const who = await screen.findByTestId("who-voted");
    expect(within(who).getByRole("tab", { name: "Validators 2 of 3" })).toHaveAttribute("aria-selected", "true");
    const rows = await within(who).findAllByTestId("validator-vote");
    expect(rows.map((r) => r.textContent?.replace(/\d+ \w+ ago|in \d+ \w+/, ""))).toEqual([
      "TNThamar NodeYes10.4%",
      "HOHoneycombYes12.3%",
    ]);
    expect(await within(who).findByTestId("not-voted")).toHaveTextContent(
      "1 validator has not voted yet:DFDeep Fields· their stakers can still vote for themselves",
    );

    await userEvent.click(within(who).getByRole("tab", { name: "Everyone 5" }));
    expect(await within(who).findAllByTestId("vote-row")).toHaveLength(5);
    expect(within(who).getAllByTestId("vote-row")[0]).toHaveTextContent("Split vote");
    await userEvent.click(within(who).getByRole("button", { name: "No" }));
    await waitFor(() => expect(within(who).getAllByTestId("vote-row")).toHaveLength(1));
  });

  it("renders a passed community pool spend with its funds box, block and final result", async () => {
    renderWithProviders(<ProposalPage id={45} />, { api: api(p45) });
    const texts = await tiles();
    expect(screen.getByTestId("proposal-status")).toHaveAttribute("data-tag", "ok");
    await waitFor(() =>
      expect(texts().slice(0, 4)).toEqual([
        "ResultPassedDec 13, 2025 · executed in block 21,512,345",
        "Yes100.0%103.5M BZE · more than 50% needed",
        "Turnout70.4%33.4% needed",
        "VotingDec 8 → Dec 13, 20255 days · ended",
      ]),
    );
    expect(texts()[4]).toMatch(/^Proposed by.*bze1m3k…9ds4Submitted Dec 8, 2025, 10:20 UTC$/);
    expect(within(screen.getByTestId("summary-tiles")).getByRole("link", { name: "21,512,345" })).toHaveAttribute("href", "/blocks/21512345");
    expect(screen.getByTestId("funds-box")).toHaveTextContent("Community pool module3,000,000 BZE");
    const done = screen.getByTestId("spend-done");
    expect(done).toHaveTextContent("Done by the chain in block 21,512,345, right when voting ended.");
    expect(within(done).getByRole("link", { name: "21,512,345" })).toHaveAttribute("href", "/blocks/21512345");
    expect(screen.getByTestId("votes-card")).toHaveTextContent("Final result");
    expect(screen.queryByTestId("want-to-vote")).not.toBeInTheDocument();
    expect(await screen.findByTestId("votes-summary")).toHaveTextContent(
      "1 validator voted Yes. 2 accounts voted on their own.",
    );
    await userEvent.click(screen.getByRole("button", { name: "See every vote" }));
    expect(await screen.findByRole("tab", { name: "Everyone 3" })).toBeInTheDocument();
    // Voting ended: today's active set is not the one that could vote, so no
    // "of N" and no "did not vote" in the counts (the summary and the tab once
    // disagreed); the validators that did not vote are named from the set the
    // tally counted, as recorded.
    expect(screen.getByRole("tab", { name: "Validators 1" })).toBeInTheDocument();
    const notVoted = await screen.findByTestId("not-voted");
    expect(notVoted).toHaveTextContent(/^3 of the 4 active validators did not vote:/);
    expect(notVoted).not.toHaveTextContent("today's");
    expect(notVoted).not.toHaveTextContent("their stakers can still vote");
    expect(screen.queryByTestId("non-voters-pending")).not.toBeInTheDocument();

    // Indexed votes and a synced turnout: nothing is marked historical.
    expect(await screen.findAllByTestId("validator-vote")).toHaveLength(1);
    expect(screen.queryByTestId("historical-turnout")).not.toBeInTheDocument();
    expect(screen.queryByTestId("historical-vote")).not.toBeInTheDocument();
    expect(screen.queryByTestId("historical-explainer")).not.toBeInTheDocument();
  });

  it("names the recorded non-voters, largest power first, with an unknown validator by its consensus address", async () => {
    renderWithProviders(<ProposalPage id={45} />, { api: api(p45) });
    await userEvent.click(await screen.findByRole("button", { name: "See every vote" }));
    const rows = await within(await screen.findByTestId("not-voted")).findAllByTestId("non-voter");
    expect(rows.map((r) => r.textContent)).toEqual(["TNThamar Node30.2%", "DFDeep Fields8.1%", "C3B2…0A9F1.2%"]);
    expect(within(rows[0]).getByRole("link", { name: "Thamar Node" })).toHaveAttribute(
      "href",
      "/validators/bzevaloper1x7ym3n6d9kjwz0q4u5r2a8l4tqs9vfmyh2gz3e",
    );
    expect(within(rows[2]).queryByRole("link")).not.toBeInTheDocument();
    expect(within(rows[2]).getByTitle("C3B2A1F0E9D8C7B6A5948372615F4E3D2C1B0A9F")).toBeInTheDocument();
  });

  it("falls back to today's active set, said so, while the set at the time is not recorded", async () => {
    const nonVoters = vi.fn();
    renderWithProviders(<ProposalPage id={45} />, { api: api({ ...p45, active_set: null }, { proposalNonVoters: nonVoters }) });
    await userEvent.click(await screen.findByRole("button", { name: "See every vote" }));
    const notVoted = await screen.findByTestId("not-voted");
    expect(notVoted).toHaveTextContent(/^2 of today's active validators did not vote:/);
    expect(within(notVoted).getByTestId("non-voters-pending")).toHaveTextContent("The exact list is being recorded.");
    expect(within(notVoted).queryByTestId("non-voter")).not.toBeInTheDocument();
    expect(nonVoters).not.toHaveBeenCalled();
  });

  it("says every validator of the recorded set voted, without asking for the list", async () => {
    const nonVoters = vi.fn();
    const p = { ...p45, active_set: { height: 21512346, size: 4, non_voters_count: 0 } };
    renderWithProviders(<ProposalPage id={45} />, { api: api(p, { proposalNonVoters: nonVoters }) });
    await userEvent.click(await screen.findByRole("button", { name: "See every vote" }));
    expect(await screen.findByTestId("not-voted")).toHaveTextContent(/^All 4 active validators voted\.$/);
    expect(nonVoters).not.toHaveBeenCalled();
  });

  it("marks what the archive read as historical: the turnout, the votes and the deposit", async () => {
    renderWithProviders(<ProposalPage id={43} />, { api: api(p43) });
    const texts = await tiles();
    // The Turnout tile's chip, then the turnout check's once the voting rules load.
    await waitFor(() => expect(screen.getAllByTestId("historical-turnout")).toHaveLength(2));
    expect(texts()[2]).toBe("Turnout67.3%33.4% needed · historical");
    const [tile, check] = screen.getAllByTestId("historical-turnout");
    expect(tile).toHaveAttribute("title", expect.stringContaining("after the fact"));
    expect(within(screen.getByTestId("votes-card")).getByTestId("historical-turnout")).toBe(check);
    expect(await screen.findByTestId("historical-explainer")).toHaveTextContent(
      "Some votes were read from the chain's state at the end of voting (marked historical).",
    );

    const who = screen.getByTestId("who-voted");
    await userEvent.click(within(who).getByRole("button", { name: "See every vote" }));
    const [validatorVote] = await within(who).findAllByTestId("validator-vote");
    const mark = within(validatorVote).getByRole("link", { name: "historical" });
    expect(mark).toHaveAttribute("href", "/blocks/17131436");
    expect(mark).toHaveAttribute("title", "Vote as recorded on chain when voting ended; the voting transaction itself is not indexed.");
    expect(validatorVote).not.toHaveTextContent(/ago/);

    await userEvent.click(within(who).getByRole("tab", { name: "Everyone 27" }));
    const rows = await within(who).findAllByTestId("vote-row");
    expect(rows).toHaveLength(3);
    for (const row of rows) {
      expect(within(row).getByRole("link", { name: "historical" })).toHaveAttribute("href", "/blocks/17131436");
      expect(row).not.toHaveTextContent(/ago/);
      expect(within(row).queryByRole("link", { name: /ago/ })).not.toBeInTheDocument();
    }

    await userEvent.click(screen.getByRole("button", { name: /More details/ }));
    const deposits = await screen.findByTestId("deposits");
    expect(deposits).toHaveTextContent("100,000 BZE in total · historical · by");
    expect(within(deposits).queryByRole("link", { name: /tx|ago/i })).not.toBeInTheDocument();
  });

  it("explains the historical rows when only a deposit came from the archive", async () => {
    const archived: Page<ProposalDeposit> = { items: [{ ...deposits45.items[0], source: "archive" } as ProposalDeposit], next_cursor: null };
    renderWithProviders(<ProposalPage id={45} />, { api: api(p45, { proposalDeposits: () => Promise.resolve(archived) }) });
    expect(await screen.findByTestId("historical-explainer")).toBeInTheDocument();
    expect(screen.queryByTestId("historical-turnout")).not.toBeInTheDocument();
  });

  it("sums up a rejected proposal in red, with no execution", async () => {
    renderWithProviders(<ProposalPage id={45} />, { api: api(rejected) });
    const texts = await tiles();
    await waitFor(() =>
      expect(texts().slice(0, 3)).toEqual([
        "ResultRejectedDec 13, 2025 · in block 21,512,345",
        "Yes0.0%0 BZE · more than 50% needed",
        "Turnout70.4%33.4% needed",
      ]),
    );
    expect(screen.getByTestId("proposal-status")).toHaveTextContent("Rejected");
    expect(screen.getByTestId("proposal-status")).toHaveAttribute("data-tag", "bad");
  });

  it("keeps what the options mean collapsed until asked", async () => {
    renderWithProviders(<ProposalPage id={45} />, { api: api(p45) });
    const votes = await screen.findByTestId("votes-card");
    const options = within(votes).getByTestId("vote-options");
    const button = within(votes).getByRole("button", { name: "What the options mean" });
    expect(options).not.toBeVisible();
    expect(button).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(button);
    expect(options).toBeVisible();
    expect(options).toHaveTextContent("No with vetoyou think it is harmful or spam.");
  });

  it("keeps message types, deposits and the raw proposal in More details", async () => {
    renderWithProviders(<ProposalPage id={45} />, { api: api(p45) });
    await userEvent.click(await screen.findByRole("button", { name: /More details/ }));
    expect(screen.getByText("/cosmos.distribution.v1beta1.MsgCommunityPoolSpend")).toBeInTheDocument();
    expect(await screen.findByTestId("deposits")).toHaveTextContent("100,000 BZE by");
    expect(screen.getByTestId("deposits")).toHaveTextContent("· returned");
    expect(screen.getByText(/The chain's final tally/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy JSON" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open on Ping.pub/ })).toHaveAttribute("href", expect.stringMatching(/\/gov\/45$/));
  });

  it("answers not found for a proposal the API does not know", async () => {
    const proposal = vi.fn(() => Promise.reject(new ApiError(404, "not_found", "proposal 99 is not known")));
    renderWithProviders(<ProposalPage id={99} />, { api: api(p48, { proposal }) });
    expect(await screen.findByRole("heading", { name: "Proposal not found" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Proposal #99");
    expect(screen.getByTestId("proposal-not-found")).toHaveTextContent("The explorer does not know proposal #99.");
    expect(screen.getByRole("link", { name: "proposals list" })).toHaveAttribute("href", "/proposals");
  });

  it("offers a retry when the API fails", async () => {
    const proposal = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<ProposalPage id={48} />, { api: api(p48, { proposal }) });
    await userEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(proposal).toHaveBeenCalledTimes(2);
  });
});
