import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { Dao, DaoList, Page, TokenSummary } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import daosList from "../../../test/fixtures/api/v1/daos.json";
import dao1 from "../../../test/fixtures/api/v1/daos/1.json";
import dao2 from "../../../test/fixtures/api/v1/daos/2.json";
import history1 from "../../../test/fixtures/api/v1/daos/1/history.json";
import history2 from "../../../test/fixtures/api/v1/daos/2/history.json";
import proposals1 from "../../../test/fixtures/api/v1/daos/1/proposals.json";
import proposals2 from "../../../test/fixtures/api/v1/daos/2/proposals.json";
import tokens from "../../../test/fixtures/api/v1/tokens.json";

import { DaoPage } from "./dao-page";

const byId: Record<number, Dao> = { 1: dao1 as Dao, 2: dao2 as Dao };
const histories = { 1: history1, 2: history2 };

function api(extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    dao: (id) => (byId[id] ? Promise.resolve(byId[id]) : Promise.reject(new ApiError(404, "not_found", "no"))),
    daoHistory: (id) => Promise.resolve(histories[id as 1 | 2]),
    daoProposals: (id) => Promise.resolve(id === 1 ? proposals1 : proposals2),
    daos: () => Promise.resolve(daosList as DaoList),
    tokens: () => Promise.resolve(tokens as Page<TokenSummary>),
    ...extra,
  });
}

const open = async (id: number, tab?: string, extra: Partial<ExplorerApi> = {}) => {
  renderWithProviders(<DaoPage id={id} initialTab={tab} />, { api: api(extra) });
  await screen.findByTestId("dao-tiles");
};

const tab = (name: string) => screen.getByRole("tab", { name: new RegExp(`^${name}`) });

describe("DaoPage", () => {
  it("shows a skeleton on the first load", () => {
    renderWithProviders(<DaoPage id={1} />, { api: pendingApi() });
    expect(screen.getByTestId("dao-skeleton")).toBeInTheDocument();
  });

  it("says not found on a 404 and offers the list", async () => {
    renderWithProviders(<DaoPage id={99} />, { api: api() });
    const box = await screen.findByTestId("dao-not-found");
    expect(box).toHaveTextContent("DAO #99");
    expect(within(box).getByRole("link", { name: "DAOs list" })).toHaveAttribute("href", "/daos");
  });

  it("shows an error state for any other failure", async () => {
    renderWithProviders(<DaoPage id={1} />, { api: api({ dao: () => Promise.reject(new ApiError(502, "upstream_error", "x")) }) });
    expect(await screen.findByTestId("dao-error")).toBeInTheDocument();
  });

  it("says Self-governed, with no admin chain, for a DAO that owns itself", async () => {
    const own: Dao = { ...(dao1 as Dao), admin: dao1.address, admin_label: null, self_governed: true };
    await open(1, undefined, { dao: () => Promise.resolve(own) });
    expect(screen.getByTestId("dao-admin-state")).toHaveTextContent("Self-governed");
    expect(screen.queryByTestId("dao-admin-chain")).toBeNull();
  });

  describe("a member-list DAO run by an account", () => {
    it("heads the page with name, number, type, address and the app link", async () => {
      await open(1);
      expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Honey Council");
      expect(screen.getByTestId("dao-id")).toHaveTextContent("DAO #1");
      expect(screen.getByTestId("dao-type")).toHaveTextContent("Member list");
      expect(screen.getByTestId("dao-status")).toHaveTextContent(/^Run by/);
      expect(within(screen.getByTestId("dao-status")).getByRole("link")).toHaveAttribute("href", `/accounts/${dao1.admin}`);
      expect(screen.getByTestId("dao-address")).toHaveTextContent(dao1.address);
      expect(screen.getByTestId("dao-app-link")).toHaveAttribute("href", "https://dao.getbze.com/dao/1");
      expect(screen.getByTestId("dao-app-link")).toHaveAttribute("target", "_blank");
      expect(screen.queryByTestId("dao-admin-chain")).toBeNull();
      expect(screen.queryByTestId("dao-handover")).toBeNull();
    });

    it("has the four tiles: treasury, members, proposal cost and voting period", async () => {
      await open(1);
      const tiles = within(screen.getByTestId("dao-tiles")).getAllByTestId("tile");
      expect(tiles).toHaveLength(4);
      await waitFor(() => expect(tiles[0]).toHaveTextContent("Treasury≈ $185.00"));
      expect(tiles[0]).toHaveTextContent("2 tokens");
      expect(tiles[1]).toHaveTextContent("Members3");
      expect(tiles[1]).toHaveTextContent("one vote each");
      expect(tiles[2]).toHaveTextContent("Proposal cost");
      expect(tiles[2]).toHaveTextContent("BZE");
      expect(tiles[2]).toHaveTextContent("deposit");
      expect(tiles[3]).toHaveTextContent("Voting period3 days");
      expect(tiles[3]).toHaveTextContent("expedited: 1 day");
    });

    it("orders the tabs Treasury, Members, Proposals, Rules, History", async () => {
      await open(1);
      expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Treasury 2", "Members 3", "Proposals", "Rules", "History"]);
    });

    it("shows the treasury balances and the account link first", async () => {
      await open(1);
      expect(await screen.findAllByTestId("token-row")).toHaveLength(2);
      expect(screen.getByTestId("treasury-account-link")).toHaveAttribute("href", `/accounts/${dao1.address}`);
    });

    it("lists the members", async () => {
      await open(1);
      await userEvent.click(tab("Members"));
      expect(screen.getAllByTestId("dao-member")).toHaveLength(3);
      expect(screen.getByRole("tabpanel")).toHaveTextContent("3 members, one vote each.");
    });

    it("lists the DAO's proposals in the Proposals tab", async () => {
      await open(1);
      await userEvent.click(tab("Proposals"));
      expect(await screen.findAllByTestId("dao-proposal-row")).toHaveLength(3);
    });

    it("explains each rule in plain words with the raw value on hover", async () => {
      await open(1);
      await userEvent.click(tab("Rules"));
      const rules = screen.getByTestId("dao-rules");
      expect(rules).toHaveTextContent("40% of the voting power must vote");
      expect(within(rules).getByText("40% of the voting power must vote")).toHaveAttribute("title", "0.400000000000000000");
      expect(rules).toHaveTextContent("More than 50% of the yes and no votes must be yes");
      expect(rules).toHaveTextContent("3 days");
      expect(rules).toHaveTextContent("Allowed: 1 day, needs 60% of the voting power to vote and more than 66% of it to say yes");
      expect(rules).toHaveTextContent("Only voters of the DAO");
      expect(rules).toHaveTextContent("burned");
      expect(rules).toHaveTextContent("Refunded only if the proposal passes; a kept deposit is kept in the DAO's treasury");
      expect(screen.getByTestId("dao-description")).toHaveTextContent("Spends the honey fund on the hive.");
    });

    it("writes the history newest first, one line per change", async () => {
      await open(1);
      await userEvent.click(tab("History"));
      const rows = await screen.findAllByTestId("dao-history-row");
      expect(rows[0]).toHaveTextContent("Members added:");
      expect(rows[1]).toHaveTextContent("Rules changed: quorum, threshold");
      expect(rows[2]).toHaveTextContent("Created by");
      expect(within(rows[2]).getByRole("link", { name: /ABAB/i })).toHaveAttribute("href", `/txs/${"AB".repeat(32)}`);
    });

    it("keeps the tab the URL asked for and ends with the raw JSON", async () => {
      await open(1, "rules");
      expect(tab("Rules")).toHaveAttribute("aria-selected", "true");
      await userEvent.click(screen.getByRole("button", { name: /More details/ }));
      expect(await screen.findByText(/"admin_seq": 0/)).toBeInTheDocument();
    });
  });

  describe("a stake DAO run by another DAO", () => {
    it("names its admin chain and the pending handover", async () => {
      await open(2);
      expect(screen.getByTestId("dao-type")).toHaveTextContent("Stakers");
      expect(screen.getByTestId("dao-status")).toHaveTextContent(/Run by.*DAO #1 · Honey Council/);
      expect(screen.getByTestId("dao-handover")).toHaveTextContent("Admin handover pending to");
      // The chain runs on past the DAO that runs this one, to the account that ends it.
      const chain = screen.getByTestId("dao-admin-chain");
      expect(chain).toHaveTextContent(/Run by.*DAO #1 · Honey Council.*, which is run by.*bze1a56/);
      expect(within(chain).getByTestId("dao-chip")).toHaveAttribute("href", "/daos/1");
      expect(within(screen.getByTestId("dao-status")).getByTestId("dao-chip")).toHaveAttribute("href", "/daos/1");
    });

    it("shows the voting stake and free proposals in the tiles", async () => {
      await open(2);
      const tiles = within(screen.getByTestId("dao-tiles")).getAllByTestId("tile");
      expect(tiles[1]).toHaveTextContent("Voting stake");
      expect(tiles[1]).toHaveTextContent("9M BZE");
      expect(tiles[1]).toHaveTextContent("in 2 programs");
      expect(tiles[2]).toHaveTextContent("free");
      expect(tiles[3]).toHaveTextContent("no expedited voting");
    });

    it("explains the electorate and links each staking token", async () => {
      await open(2);
      expect(tab("Electorate")).toBeInTheDocument();
      await userEvent.click(tab("Electorate"));
      expect(screen.getByTestId("electorate-explainer")).toHaveTextContent(
        "Voting power = stake in these programs, counted only if it joined before the proposal.",
      );
      const [sr, dr] = screen.getAllByTestId("dao-source");
      expect(sr).toHaveTextContent("Staking reward 3");
      expect(sr).toHaveTextContent("9,000,000 BZE");
      // One link per width: under the program on phones, in its own column from c2.
      expect(within(sr).getAllByRole("link", { name: "BZE" }).map((l) => l.getAttribute("href"))).toEqual(["/tokens/ubze", "/tokens/ubze"]);
      expect(within(sr).getByTestId("source-token-phone")).toBeInTheDocument();
      expect(dr).toHaveTextContent("Denom reward of factory/");
      expect(within(dr).getAllByRole("link")[0]).toHaveAttribute("href", expect.stringContaining("/tokens/factory%2F"));
    });

    it("says a proposal costs nothing and shows who can propose", async () => {
      await open(2);
      await userEvent.click(tab("Rules"));
      expect(screen.getByTestId("dao-rules")).toHaveTextContent("Anyone");
      expect(screen.getByTestId("dao-rules")).toHaveTextContent("Free");
      expect(screen.getByTestId("dao-rules")).toHaveTextContent("Not allowed");
    });

    it("writes the handover offer in its history", async () => {
      await open(2, "history");
      const rows = await screen.findAllByTestId("dao-history-row");
      expect(rows[0]).toHaveTextContent("Admin handover offered to");
      expect(rows[1]).toHaveTextContent("Created by");
    });
  });

  it("says the live data is unavailable and keeps the history, when the node did not answer", async () => {
    const down: Dao = { ...(dao1 as Dao), live: { available: false }, controller: null, treasury: [] };
    await open(1, undefined, { dao: () => Promise.resolve(down) });
    expect(screen.getByTestId("live-unavailable")).toHaveTextContent("Live data is unavailable right now");
    expect(within(screen.getByTestId("dao-tiles")).getAllByTestId("tile")[0]).toHaveTextContent("—");
    await userEvent.click(tab("Rules"));
    expect(screen.getByTestId("tab-unavailable")).toHaveTextContent("The rules will show once the node answers again.");
    await userEvent.click(tab("History"));
    expect(await screen.findAllByTestId("dao-history-row")).toHaveLength(3);
  });

  it("shows history skeletons, then an empty line for a DAO without changes", async () => {
    await open(1, "history", { daoHistory: () => Promise.resolve({ items: [], next_cursor: null }) });
    expect(await screen.findByTestId("dao-history-empty")).toBeInTheDocument();
  });
});
