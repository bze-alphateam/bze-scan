import { readFileSync } from "node:fs";
import path from "node:path";

import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderWithProviders } from "@/test/render";

import { createRenderContext, rendererFor } from "./registry";
import type { RenderMessage } from "./types";

const DIR = path.resolve("test/fixtures/messages") + path.sep;
const fixture = (name: string): RenderMessage => ({ msg_index: 0, ...JSON.parse(readFileSync(`${DIR}${name}.json`, "utf8")) });
const A = "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw";
const HONEY = `factory/${A}/uhoney`;

const ctx = createRenderContext({
  transfers: [],
  tokens: new Map([
    ["ubze", { symbol: "BZE", exponent: 6, price_usd: null }],
    [HONEY, { symbol: "HONEY", exponent: 6, price_usd: null }],
  ]),
});

function show(name: string, context = ctx) {
  const msg = fixture(name);
  const r = rendererFor(msg.type_url);
  return renderWithProviders(<r.Body msg={msg} ctx={context} />);
}

describe("DAO messages", () => {
  it("renders the nested actions of an admin execution through the registry", () => {
    show("dao-admin-execute");
    const inner = screen.getAllByTestId("nested-message");
    expect(inner).toHaveLength(2);
    expect(within(inner[0]).getByText("1. Send 5 BZE to bze1m3k…9ds4")).toBeInTheDocument();
    expect(within(inner[1]).getByText(/^2\. Mint 1 HONEY to/)).toBeInTheDocument();
  });

  it("shows what a proposal would do, with its nested messages", () => {
    show("dao-submit-proposal");
    expect(screen.getByText("What it would do")).toBeInTheDocument();
    expect(within(screen.getByTestId("nested-message")).getByText("Send 5 BZE to bze1m3k…9ds4")).toBeInTheDocument();
    expect(screen.getByText("Pay 5 BZE to the market maker")).toBeInTheDocument();
  });

  it("reads the settle outcomes from the message's events, in the title", () => {
    const title = (m: RenderMessage) => rendererFor(m.type_url).title(m, ctx);
    expect(title(fixture("dao-settle-proposal"))).toBe("Settled DAO proposal #12 · passed and executed");
    expect(title(fixture("dao-settle-proposal-rejected"))).toBe("Settled DAO proposal #12 · rejected");
    expect(title(fixture("dao-settle-proposal-failed"))).toBe("Settled DAO proposal #12 · passed, but its actions failed");
    expect(title({ ...fixture("dao-settle-proposal"), events: [] })).toBe("Settled DAO proposal #12");
    const expired = { ...fixture("dao-settle-proposal"), events: [{ type: "bze.dao.EventProposalSettled", attrs: { status: 4 } }] };
    expect(title(expired)).toBe("Settled DAO proposal #12 · expired");
  });

  it("explains why the actions of a settled proposal failed", () => {
    show("dao-settle-proposal-failed");
    expect(screen.getByText("insufficient funds")).toBeInTheDocument();
  });

  it("reads each nested message's own events, and none when they cannot be told apart", () => {
    const pool = (id: string) => ({ pool_id: id, fee: "0.005", fee_dest: { treasury: "1", burner: "0" } });
    const event = (id: string) => ({
      type: "bze.tradebin.PoolFeesUpdatedEvent",
      attrs: { pool_id: id, old_fee: "0.003", new_fee: "0.005", old_fee_dest: { treasury: "1", burner: "0" }, new_fee_dest: { treasury: "1", burner: "0" } },
    });
    const nested = (id: string) => ({ "@type": "/bze.tradebin.MsgUpdatePools", authority: A, updates: [pool(id)] });
    const run = (msgs: object[]) => ({ ...fixture("dao-admin-execute"), body: { admin: A, dao_id: "3", msgs }, events: [event("p1"), event("p2")] });
    const render = (m: RenderMessage) => {
      const r = rendererFor(m.type_url);
      renderWithProviders(<r.Body msg={m} ctx={ctx} />);
    };
    // Two siblings of one type: the transaction's events cannot be attributed, so neither shows an outcome from them.
    render(run([nested("p1"), nested("p2")]));
    expect(screen.queryByText(/→/)).toBeNull();
    expect(screen.getAllByText(/new fee 0\.5%/)).toHaveLength(2);
  });

  it("shows a single nested message's events", () => {
    const nested = { "@type": "/bze.tradebin.MsgUpdatePools", authority: A, updates: [{ pool_id: "p1", fee: "0.005" }] };
    const event = { type: "bze.tradebin.PoolFeesUpdatedEvent", attrs: { pool_id: "p1", old_fee: "0.003", new_fee: "0.005" } };
    const m = { ...fixture("dao-admin-execute"), body: { admin: A, dao_id: "3", msgs: [nested] }, events: [event] };
    const r = rendererFor(m.type_url);
    renderWithProviders(<r.Body msg={m} ctx={ctx} />);
    expect(screen.getByText("0.3% → 0.5%")).toBeInTheDocument();
  });

  it("shows a DAO's voting period as a duration", () => {
    show("dao-create-dao");
    expect(screen.getByText("3 days")).toBeInTheDocument();
  });

  it("words the DAO module's parameters from the parameters dictionary", () => {
    const msg = {
      ...fixture("dao-update-params"),
      body: { authority: A, params: { dao_creation_fee: { denom: "ubze", amount: "1000000000" }, dao_creation_fee_destination: "burner", max_open_proposals: "20" } },
    };
    const r = rendererFor(msg.type_url);
    renderWithProviders(<r.Body msg={msg} ctx={ctx} />);
    expect(screen.getByText("Fee to create a DAO")).toBeInTheDocument();
    expect(screen.getByText("1,000 BZE, burned")).toBeInTheDocument();
    expect(screen.queryByText(/"denom"/)).toBeNull();
  });

  it("words the emergency key holder from the dictionary, a chip for the address, Nobody for none", () => {
    show("guardian-update-params");
    expect(screen.getByText("Emergency key holder")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "bze1m3k…9ds4" })).toBeInTheDocument();
    const none = { ...fixture("guardian-update-params"), body: { authority: A, params: { guardian: "" } } };
    const r = rendererFor(none.type_url);
    renderWithProviders(<r.Body msg={none} ctx={ctx} />);
    expect(screen.getByText("Nobody")).toBeInTheDocument();
  });

  it("lists the rules a config change touched, from the event", () => {
    show("dao-update-dao-config");
    expect(screen.getByText("min quorum, threshold")).toBeInTheDocument();
  });

  it("names a DAO's voters kind and a vote option, and survives an unknown option", () => {
    const vote = { ...fixture("dao-vote"), body: { proposal_id: "12", option: 2 } };
    expect(rendererFor(vote.type_url).title(vote, ctx)).toBe("Voted No on DAO proposal #12");
    const odd = { ...fixture("dao-vote"), body: { proposal_id: "12", option: "WEIRD" } };
    expect(rendererFor(odd.type_url).title(odd, ctx)).toBe("Voted Weird on DAO proposal #12");
    const stake = { ...fixture("dao-create-dao"), body: { name: "Stakers", type: 1 } };
    expect(rendererFor(stake.type_url).title(stake, ctx)).toBe("Created DAO “Stakers” (staking vote)");
  });

  it("words one-sided member changes", () => {
    const only = (body: object) => rendererFor("/bze.dao.MsgUpdateMembers").title({ ...fixture("dao-update-members"), body: { dao_id: "3", ...body } }, ctx);
    expect(only({ add: ["bze1a"] })).toBe("Added 1 member to DAO #3");
    expect(only({ remove: ["bze1a", "bze1b"] })).toBe("Removed 2 members from DAO #3");
  });
});

describe("guardian messages", () => {
  it.each(["guardian-tighten-rate-limit", "guardian-block-denom", "guardian-halt-chain", "guardian-pause-automation", "guardian-close-channel"])(
    "%s carries the Emergency badge and an Emergency title",
    (name) => {
      show(name);
      expect(screen.getByTestId("emergency-badge")).toHaveTextContent("Emergency");
      const msg = fixture(name);
      expect(rendererFor(msg.type_url).title(msg, ctx)).toMatch(/^Emergency: /);
    },
  );

  it.each(["guardian-unblock-denom", "guardian-resume-automation", "guardian-reopen-channel"])("%s, a governance decision, has no badge", (name) => {
    show(name);
    expect(screen.queryByTestId("emergency-badge")).toBeNull();
  });

  it("shows the old and the new limit from the event", () => {
    show("guardian-tighten-rate-limit");
    expect(screen.getByText("10% → 1%")).toBeInTheDocument();
    expect(screen.getByText("10% → 0%")).toBeInTheDocument();
  });

  it("lists the switched-off kinds of actions in words", () => {
    show("guardian-disable-messages");
    expect(screen.getByText("Create order")).toBeInTheDocument();
    expect(screen.getByText("Multi swap")).toBeInTheDocument();
    expect(screen.getByText("Send")).toBeInTheDocument();
  });

  it("shows a halt's reason as plain text, never as markup", () => {
    const { container } = show("guardian-halt-chain");
    expect(screen.getByText("Critical bug <b>in</b> the DEX, validators stand by")).toBeInTheDocument();
    expect(container.querySelector("b")).toBeNull();
  });

  it("names the other chain of a closed channel when the transaction knows it", () => {
    const withChain = createRenderContext({
      transfers: [],
      ibcTransfers: [{ channel_id: "channel-3", counterparty: { chain_id: "cosmoshub-4", pretty_name: "Cosmos Hub" } } as never],
    });
    const msg = fixture("guardian-close-channel");
    expect(rendererFor(msg.type_url).title(msg, withChain)).toBe("Emergency: closed channel-3 to Cosmos Hub");
  });
});

describe("token and pool messages", () => {
  it("keeps the precision of a fee ratio", () => {
    const msg = { ...fixture("tokenfactory-set-denom-config"), body: { denom: HONEY, community_pool: "", transfer_fee: "0.00125", gas_surcharge: "0.010000000000000000" } };
    expect(rendererFor(msg.type_url).title(msg, ctx)).toBe("Set HONEY's community pool to none, transfer fee 0.125%, gas surcharge 1%");
  });

  it("reads zero ratios as none", () => {
    const msg = { ...fixture("tokenfactory-set-denom-config"), body: { denom: HONEY, community_pool: "", transfer_fee: "0", gas_surcharge: "0" } };
    expect(rendererFor(msg.type_url).title(msg, ctx)).toBe("Set HONEY's community pool to none, transfer fee none, gas surcharge none");
  });

  it("lists each pool's old and new fee from the event, and the unchanged ones", () => {
    show("tradebin-update-pools");
    expect(screen.getByText(/0\.3% \(100% to the treasury, 0% burned\) → 0\.5% \(50% to the treasury, 50% burned\)/)).toBeInTheDocument();
    expect(screen.getAllByText("unchanged")).toHaveLength(2);
  });

  it("shows the new fees of a proposal that has no events yet", () => {
    const msg = { ...fixture("tradebin-update-pools"), events: null };
    const r = rendererFor(msg.type_url);
    renderWithProviders(<r.Body msg={msg} ctx={ctx} />);
    expect(screen.getByText("new fee 0.5% (50% to the treasury, 50% burned)")).toBeInTheDocument();
    expect(screen.getByText("new fee 0.3% (100% to the treasury, 0% burned)")).toBeInTheDocument();
  });
});
