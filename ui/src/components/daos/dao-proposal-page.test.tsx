import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { DaoProposal, DaoVoteList, Page, TokenSummary } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import p3 from "../../../test/fixtures/api/v1/dao-proposals/3.json";
import p5 from "../../../test/fixtures/api/v1/dao-proposals/5.json";
import p6 from "../../../test/fixtures/api/v1/dao-proposals/6.json";
import p7 from "../../../test/fixtures/api/v1/dao-proposals/7.json";
import votes3 from "../../../test/fixtures/api/v1/dao-proposals/3/votes.json";
import votes7 from "../../../test/fixtures/api/v1/dao-proposals/7/votes.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";

vi.mock("@/render/messages/registry", async (importOriginal) => {
  const real = await importOriginal<typeof import("@/render/messages/registry")>();
  return {
    ...real,
    rendererFor: (typeUrl: string) =>
      typeUrl === "/test.Boom"
        ? { title: () => { throw new Error("cannot read"); }, Body: () => null }
        : real.rendererFor(typeUrl),
  };
});

import { DaoProposalPage } from "./dao-proposal-page";

const byId: Record<number, DaoProposal> = { 3: p3 as DaoProposal, 5: p5 as DaoProposal, 6: p6 as DaoProposal, 7: p7 as DaoProposal };

function api(extra: Partial<ExplorerApi> = {}, patch: Partial<DaoProposal> = {}): ExplorerApi {
  return fakeApi({
    daoProposal: (id) => (byId[id] ? Promise.resolve({ ...byId[id], ...patch }) : Promise.reject(new ApiError(404, "not_found", "no"))),
    daoProposalVotes: (id) => Promise.resolve((id === 3 ? votes3 : votes7) as DaoVoteList),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    ...extra,
  });
}

async function open(id: number, patch: Partial<DaoProposal> = {}, daoId: number | null = null, extra: Partial<ExplorerApi> = {}) {
  renderWithProviders(<DaoProposalPage id={id} daoId={daoId} />, { api: api(extra, patch) });
  await screen.findByTestId("dao-proposal-tiles");
}

const tile = (key: string) => screen.getByTestId(`tile-${key}`);
const checkOks = (group: string) => within(screen.getByTestId(`checks-${group}`)).getAllByTestId("check").map((c) => c.getAttribute("data-ok"));

describe("DaoProposalPage", () => {
  it("shows a skeleton on the first load", () => {
    renderWithProviders(<DaoProposalPage id={7} />, { api: pendingApi() });
    expect(screen.getByTestId("dao-proposal-skeleton")).toBeInTheDocument();
  });

  it("says not found on a 404", async () => {
    renderWithProviders(<DaoProposalPage id={99} daoId={2} />, { api: api() });
    const box = await screen.findByTestId("dao-proposal-not-found");
    expect(box).toHaveTextContent("proposal #99 in DAO #2");
    expect(within(box).getByRole("link")).toHaveAttribute("href", "/daos/2");
  });

  it("says not found for a proposal of another DAO", async () => {
    renderWithProviders(<DaoProposalPage id={7} daoId={1} />, { api: api() });
    expect(await screen.findByTestId("dao-proposal-not-found")).toBeInTheDocument();
  });

  it("shows an error state for any other failure", async () => {
    renderWithProviders(<DaoProposalPage id={7} />, { api: api({ daoProposal: () => Promise.reject(new ApiError(502, "upstream_error", "x")) }) });
    expect(await screen.findByTestId("dao-proposal-error")).toBeInTheDocument();
  });

  describe("a voting stake-DAO proposal", () => {
    it("heads the page with the title, number, DAO link, status and expedited tag", async () => {
      await open(7, {}, 2);
      expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Fund the hive festival");
      expect(screen.getByTestId("dao-proposal-id")).toHaveTextContent("#7");
      expect(screen.getByTestId("dao-proposal-status")).toHaveTextContent("Voting");
      expect(screen.getByTestId("expedited-tag")).toBeInTheDocument();
      expect(screen.getByTestId("dao-chip")).toHaveAttribute("href", "/daos/2");
      expect(screen.getByTestId("dao-chip")).toHaveTextContent("Hive Stakers");
    });

    it("has Yes, No, Abstain with their share of the voting power, turnout against the quorum, and the time left", async () => {
      await open(7);
      expect(tile("yes")).toHaveTextContent("Yes so far42.0%");
      expect(tile("yes")).toHaveTextContent("4.2K BZE");
      expect(tile("no")).toHaveTextContent("6.0%");
      expect(tile("abstain")).toHaveTextContent("4.0%");
      expect(tile("turnout")).toHaveTextContent("52.0%");
      expect(tile("turnout")).toHaveTextContent("40% needed · reached");
      expect(tile("time")).toHaveTextContent("Voting ends");
      expect(tile("time")).toHaveTextContent(/in \d+ days/);
    });

    it("shows the regular checks and, for an expedited proposal, the expedite bars", async () => {
      await open(7);
      expect(checkOks("regular")).toEqual(["true", "true"]);
      expect(checkOks("expedite")).toEqual(["unknown", "unknown"]);
      expect(screen.queryByTestId("settle-note")).toBeNull();
    });

    it("says what it does in plain words, as the DAO's treasury", async () => {
      await open(7);
      const what = screen.getByTestId("dao-what-it-does");
      expect(within(what).getByTestId("runs-as-dao")).toHaveTextContent("Runs as the DAO's treasury account");
      const action = within(what).getByTestId("dao-action");
      expect(action).toHaveAttribute("data-type", "/cosmos.bank.v1beta1.MsgSend");
      await waitFor(() => expect(action).toHaveTextContent(/Send 250 BZE/));
    });

    it("keeps the proposer's text collapsed, as plain text with its line breaks and no HTML", async () => {
      await open(7);
      const text = screen.getByTestId("dao-summary-text");
      expect(text).not.toBeVisible();
      await userEvent.click(screen.getByRole("button", { name: "From the proposer" }));
      expect(text).toBeVisible();
      expect(text).toHaveTextContent("<b>this is not HTML</b>");
      expect(text.querySelector("b")).toBeNull();
      expect(getComputedStyle(text).whiteSpace).toBe("pre-wrap");
    });

    it("has no outcome yet, and the raw JSON under More details", async () => {
      await open(7);
      expect(screen.queryByTestId("dao-outcome")).toBeNull();
      await userEvent.click(screen.getByRole("button", { name: /More details/ }));
      expect(await screen.findByText(/"expedite_settleable": false/)).toBeInTheDocument();
    });
  });

  describe("a voting proposal's checks", () => {
    it("shows a bar not met yet as unknown, not failed", async () => {
      await open(7, { checks: { regular: { quorum_met: false, threshold_met: false, passes: false }, expedite: { quorum_met: false, yes_met: false, passes: false } } });
      expect(checkOks("regular")).toEqual(["unknown", "unknown"]);
      expect(checkOks("expedite")).toEqual(["unknown", "unknown"]);
    });
  });

  it("links to the proposal in the DAO app", async () => {
    await open(7);
    expect(screen.getByTestId("dao-app-link")).toHaveAttribute("href", "https://dao.getbze.com/proposal/7");
    expect(screen.getByTestId("dao-app-link")).toHaveAttribute("target", "_blank");
  });

  it("shows a stake DAO's weights as base units, not votes, when the node did not name the weight denom", async () => {
    await open(7, { weight_denom: null });
    expect(tile("yes")).toHaveTextContent("4200000000 (base units)");
    expect(tile("yes")).not.toHaveTextContent("votes");
  });

  it("shows a message its renderer cannot read as raw JSON, and the rest as usual", async () => {
    const good = (byId[7].msgs as Record<string, unknown>[])[0];
    const bad = { "@type": "/test.Boom", detail: "unreadable" };
    await open(7, { msgs: [bad, good] });
    const raw = screen.getByTestId("dao-action-raw");
    expect(raw).toHaveAttribute("data-type", "/test.Boom");
    expect(raw).toHaveTextContent('"detail": "unreadable"');
    expect(screen.getAllByTestId("dao-action")).toHaveLength(1);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Fund the hive festival");
  });

  describe("a proposal past its voting end", () => {
    it("says anyone can settle it now", async () => {
      await open(7, { voting_ended: true });
      expect(screen.getByTestId("settle-note")).toHaveTextContent("Voting ended; anyone can settle it now");
      expect(tile("time")).toHaveTextContent("Ended");
    });
  });

  describe("an expedited proposal that can settle early", () => {
    it("says it can be settled early", async () => {
      await open(7, {
        expedite_settleable: true,
        checks: { regular: { quorum_met: true, threshold_met: true, passes: true }, expedite: { quorum_met: true, yes_met: true, passes: true } },
      });
      expect(screen.getByTestId("settle-note")).toHaveTextContent("Can be settled early");
      expect(checkOks("expedite")).toEqual(["true", "true"]);
    });
  });

  describe("an executed proposal", () => {
    it("shows the result, the settle transaction and the refunded deposit", async () => {
      await open(6);
      expect(screen.getByTestId("dao-proposal-status")).toHaveTextContent("Passed (executed)");
      expect(tile("time")).toHaveTextContent("Settled");
      const outcome = screen.getByTestId("dao-outcome");
      expect(outcome).toHaveTextContent("The proposal passed and its messages ran as the DAO");
      expect(within(outcome).getByTestId("settle-tx")).toHaveAttribute("href", `/txs/${"55".repeat(32)}`);
      expect(within(outcome).getByTestId("dao-deposit")).toHaveTextContent("Deposit of 1 BZE: refunded to the proposer");
      expect(screen.queryByTestId("expedited-tag")).toBeNull();
      expect(screen.queryByTestId("checks-expedite")).toBeNull();
    });
  });

  describe("a proposal whose execution failed", () => {
    it("shows the error and until when it can be retried", async () => {
      await open(5);
      expect(screen.getByTestId("dao-proposal-status")).toHaveTextContent("Failed to execute");
      const outcome = screen.getByTestId("dao-outcome");
      expect(outcome).toHaveTextContent("running its messages failed");
      expect(within(outcome).getByTestId("execution-error")).toHaveTextContent("insufficient funds");
      expect(within(outcome).getByTestId("retry-deadline")).toHaveTextContent("Anyone can retry it until Nov 12, 2099, 10:05 UTC.");
    });
  });

  describe("a rejected member-list proposal", () => {
    it("fails the threshold, counts the votes of members and says where the kept deposit went", async () => {
      await open(3);
      expect(checkOks("regular")).toEqual(["true", "false"]);
      expect(tile("yes")).toHaveTextContent("33.3%");
      expect(tile("yes")).toHaveTextContent("1 vote");
      expect(tile("no")).toHaveTextContent("2 votes");
      const outcome = screen.getByTestId("dao-outcome");
      expect(outcome).toHaveTextContent("The proposal did not pass");
      expect(within(outcome).getByTestId("dao-deposit")).toHaveTextContent("Deposit of 1 BZE: kept: sent toburned");
    });

    it("shows the message types when the messages were not recorded, or a text proposal without any", async () => {
      await open(3, { message_types: ["/cosmos.bank.v1beta1.MsgSend"] });
      expect(screen.getByTestId("dao-actions-unread")).toHaveTextContent("/cosmos.bank.v1beta1.MsgSend");
    });

    it("says a proposal without messages is a text proposal", async () => {
      await open(3);
      expect(screen.getByTestId("dao-what-it-does")).toHaveTextContent("A text proposal");
    });
  });

  describe("an expired proposal", () => {
    it("says nobody retried in time and keeps the last error", async () => {
      await open(5, { status: "expired", retry_deadline: null });
      expect(screen.getByTestId("dao-proposal-status")).toHaveTextContent("Expired");
      expect(screen.getByTestId("outcome-text")).toHaveTextContent("expired");
      expect(screen.getByTestId("execution-error")).toBeInTheDocument();
    });
  });

  describe("a cancelled proposal", () => {
    it("says the proposer cancelled it, in which block", async () => {
      await open(3, { status: "cancelled", cancelled_height: 25101700, checks: null, deposit_refunded: null });
      expect(screen.getByTestId("dao-proposal-status")).toHaveTextContent("Cancelled");
      expect(screen.getByTestId("outcome-text")).toHaveTextContent("The proposer cancelled the proposal in block 25,101,700");
      expect(tile("time")).toHaveTextContent("Cancelled");
      expect(screen.queryByTestId("dao-deposit")).toBeNull();
    });
  });

  describe("without a tally or the rules", () => {
    it("says it is not tallied yet, with unknown tiles", async () => {
      await open(7, { yes: null, no: null, abstain: null, denominator: null, checks: null });
      expect(screen.getByTestId("dao-result")).toHaveTextContent("Not tallied yet");
      expect(tile("yes")).toHaveTextContent("—");
    });

    it("says the checks are missing when the tally has no rules to measure it", async () => {
      await open(7, { checks: null });
      expect(screen.getByTestId("dao-result")).toHaveTextContent("could not be loaded, so the checks are not shown");
    });
  });

  describe("the Votes tab", () => {
    it("lists the votes with their weight in display units and a note on a reduced one", async () => {
      await open(7);
      await userEvent.click(screen.getByRole("tab", { name: /^Votes/ }));
      const rows = await screen.findAllByTestId("dao-vote-row");
      expect(rows).toHaveLength(4);
      expect(within(rows[0]).getByTestId("dao-vote-weight")).toHaveTextContent("3K BZE");
      expect(within(rows[0]).queryByTestId("vote-reduced")).toBeNull();
      const reduced = within(rows[1]).getByTestId("vote-reduced");
      expect(reduced).toHaveTextContent("counts 700 BZE: part of this stake has left since the vote was cast");
      expect(within(rows[3]).getByRole("link", { name: /Honey Council/ })).toBeInTheDocument();
    });

    it("counts one vote per member of a member list", async () => {
      await open(3);
      await userEvent.click(screen.getByRole("tab", { name: /^Votes/ }));
      const row = await screen.findByTestId("dao-vote-row");
      expect(within(row).getByTestId("dao-vote-weight")).toHaveTextContent("1 vote");
    });

    it("filters by option and asks for that option", async () => {
      const calls: (string | undefined)[] = [];
      await open(7, {}, null, {
        daoProposalVotes: (_id, params) => {
          calls.push(params.option);
          return Promise.resolve({ ...(votes7 as DaoVoteList), items: votes7.items.filter((v) => !params.option || v.option === params.option) } as DaoVoteList);
        },
      });
      await userEvent.click(screen.getByRole("tab", { name: /^Votes/ }));
      await screen.findAllByTestId("dao-vote-row");
      await userEvent.click(screen.getByRole("button", { name: "No" }));
      expect(await screen.findAllByTestId("dao-vote-row")).toHaveLength(1);
      expect(calls).toContain("no");
    });

    it("shows a skeleton, an empty note and an error", async () => {
      renderWithProviders(<DaoProposalPage id={7} />, { api: api({ daoProposalVotes: () => new Promise(() => {}) }) });
      await screen.findByTestId("dao-proposal-tiles");
      await userEvent.click(screen.getByRole("tab", { name: /^Votes/ }));
      expect(await screen.findByTestId("list-skeleton")).toBeInTheDocument();
    });

    it("says there are no votes", async () => {
      await open(7, {}, null, { daoProposalVotes: () => Promise.resolve({ items: [], next_cursor: null, weight_denom: null }) });
      await userEvent.click(screen.getByRole("tab", { name: /^Votes/ }));
      expect(await screen.findByTestId("dao-votes-empty")).toBeInTheDocument();
    });

    it("says the votes could not be loaded", async () => {
      await open(7, {}, null, { daoProposalVotes: () => Promise.reject(new ApiError(502, "upstream_error", "x")) });
      await userEvent.click(screen.getByRole("tab", { name: /^Votes/ }));
      expect(await screen.findByTestId("dao-votes-error")).toBeInTheDocument();
    });
  });
});
