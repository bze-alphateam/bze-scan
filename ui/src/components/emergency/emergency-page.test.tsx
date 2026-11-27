import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Emergency, EmergencyAction, Stats } from "@/api/types";
import { fakeApi, renderWithProviders } from "@/test/render";

import active from "../../../test/fixtures/variants/emergency-active.json";
import clear from "../../../test/fixtures/api/v1/emergency.json";
import { EmergencyPage } from "./emergency-page";

const stats = { avg_block_time_ms: 5000 } as unknown as Stats;
const api = (e: Emergency | Error, actions: EmergencyAction[] = []) =>
  fakeApi({
    emergency: async () => {
      if (e instanceof Error) throw e;
      return e;
    },
    stats: async () => stats,
    liveHalt: async () => ({ halted: false }) as never,
    emergencyActions: async () => ({ items: actions, next_cursor: null }),
    tokens: async () => ({ items: [], next_cursor: null }),
    validators: async () => ({ items: [] }) as never,
  });

describe("EmergencyPage status", () => {
  it("says all is normal when nothing is switched", async () => {
    renderWithProviders(<EmergencyPage />, { api: api(clear as Emergency) });
    const card = await screen.findByTestId("emergency-status-card");
    expect(card).toHaveTextContent("No emergency measures are active");
    expect(card).toHaveTextContent("Checked at block 25,120,450");
    expect(screen.getByTestId("emergency-normal")).toHaveTextContent("Everything is normal");
    expect(screen.queryByTestId("emergency-halted")).toBeNull();
  });

  it("lists every active switch in plain words", async () => {
    renderWithProviders(<EmergencyPage />, { api: api(active as Emergency) });
    expect(await screen.findByTestId("emergency-status-card")).toHaveTextContent("6 emergency measures are active");
    const halted = screen.getByTestId("emergency-halted");
    expect(within(halted).getByRole("link", { name: "USDC" })).toHaveAttribute("href", "/tokens/factory%2Fbze1abc%2Fuusdc");
    expect(within(halted).getByRole("link", { name: "uother" })).toBeInTheDocument();
    expect(halted).toHaveTextContent("You cannot trade these on the DEX until governance lifts the halt.");
    expect(screen.getByTestId("emergency-blocked")).toHaveTextContent("ATOM");
    expect(screen.getByRole("link", { name: "Emergency key" })).toHaveAttribute("href", "/accounts/bze1guardian0000000000000000000000000q4xk7a");
    const channels = screen.getByTestId("emergency-channels");
    expect(channels).toHaveTextContent("channel-0 to Osmosis");
    expect(channels).toHaveTextContent("incoming ones are refunded by the sending chain");
    expect(screen.getByTestId("emergency-messages")).toHaveTextContent("Send");
    expect(screen.getByTestId("emergency-messages")).toHaveTextContent("Transactions with these actions fail.");
    expect(screen.getByTestId("emergency-automation")).toHaveTextContent("Fee conversion and burning paused");
    const halt = screen.getByTestId("emergency-halt");
    expect(halt).toHaveTextContent("Chain halt scheduled at block 25,120,900");
    expect(halt).toHaveTextContent("pool drain under way");
    expect(halt).toHaveTextContent("in about 37 min (450 blocks)");
    expect(halt).not.toHaveTextContent("stopped");
    expect(screen.queryByTestId("emergency-normal")).toBeNull();
  });

  it("collapses the inactive switches under 'Everything else is normal'", async () => {
    renderWithProviders(<EmergencyPage />, {
      api: api({ ...(clear as Emergency), active: true, automation_paused: true }),
    });
    expect(await screen.findByTestId("emergency-automation")).toBeInTheDocument();
    const normal = screen.getByTestId("emergency-normal");
    expect(normal).toHaveTextContent("Everything else is normal: Trading");
    expect(normal).not.toHaveTextContent("Fee conversion");
  });

  it("links a DAO guardian to its page", async () => {
    renderWithProviders(<EmergencyPage />, {
      api: api({ ...(clear as Emergency), guardian: { address: "bze1dao", label: "DAO #3 · Ops", label_kind: "dao" } }),
    });
    expect(await screen.findByRole("link", { name: "DAO #3 · Ops" })).toHaveAttribute("href", "/daos/3");
    expect(screen.getByTestId("emergency-status-card")).toHaveTextContent("DAO");
  });

  it("does not take a label that merely looks like a DAO for one", async () => {
    renderWithProviders(<EmergencyPage />, {
      api: api({ ...(clear as Emergency), guardian: { address: "bze1x", label: "DAO #3 · Ops", label_kind: "known" } }),
    });
    expect(await screen.findByRole("link", { name: "DAO #3 · Ops" })).toHaveAttribute("href", "/accounts/bze1x");
  });

  it("says the chain is stopped once the halt height is reached", async () => {
    renderWithProviders(<EmergencyPage />, { api: api({ ...(active as Emergency), height: 25120900 }) });
    const halt = await screen.findByTestId("emergency-halt");
    expect(halt).toHaveTextContent("The chain is stopped at block 25,120,900");
    expect(halt).not.toHaveTextContent("scheduled");
    expect(halt).not.toHaveTextContent("The chain stops");
  });

  it("says the chain is stopped when the live watch sees the emergency halt", async () => {
    const live = { halted: true, kind: "emergency" } as never;
    renderWithProviders(<EmergencyPage />, { api: Object.assign(api(active as Emergency), { liveHalt: async () => live }) });
    expect(await screen.findByText("The chain is stopped at block 25,120,900")).toBeInTheDocument();
  });

  it("says the switches arrive with v8.3.0 on an older node", async () => {
    renderWithProviders(<EmergencyPage />, { api: api({ ...(clear as Emergency), supported: false, guardian: null }) });
    expect(await screen.findByTestId("emergency-unsupported")).toHaveTextContent("Emergency switches arrive with chain v8.3.0");
  });

  it("shows a read-failed state when the state cannot be read", async () => {
    renderWithProviders(<EmergencyPage />, { api: api(new Error("502")) });
    expect(await screen.findByTestId("emergency-read-failed")).toHaveTextContent("could not be read");
  });
});

describe("EmergencyPage history", () => {
  const rows: EmergencyAction[] = [
    {
      height: 300,
      time: "2026-11-20T10:00:00Z",
      tx_hash: null,
      action: "unhalt_trading",
      by: "governance",
      signer: "bze1gov",
      details: { denoms: ["a", "b"] },
      proposal_id: 52,
      via: null,
    },
    {
      height: 200,
      time: "2026-11-19T10:00:00Z",
      tx_hash: "abc123",
      action: "halt_trading",
      by: "guardian",
      signer: "bze1guardian",
      details: { denoms: ["factory/bze1abc/uusdc"] },
      proposal_id: null,
      via: null,
    },
    {
      height: 150,
      time: "2026-11-18T10:00:00Z",
      tx_hash: null,
      action: "close_channel",
      by: "governance",
      signer: null,
      details: { channel_id: "channel-3" },
      proposal_id: null,
      via: null,
    },
  ];

  it("titles each row, tells who acted and links the proposal, the transaction or the block", async () => {
    renderWithProviders(<EmergencyPage />, { api: api(clear as Emergency, rows) });
    fireEvent.click(await screen.findByTestId("emergency-tab-history"));
    const items = await screen.findAllByTestId("emergency-action");
    expect(items).toHaveLength(3);

    expect(items[0]).toHaveTextContent("Lifted the trading halt of 2 tokens");
    expect(within(items[0]).getByTestId("emergency-by")).toHaveTextContent("by governance");
    expect(within(items[0]).getByRole("link", { name: "Proposal #52" })).toHaveAttribute("href", "/proposals/52");

    expect(items[1]).toHaveTextContent("Emergency: halted trading of uusdc");
    expect(within(items[1]).getByTestId("emergency-by")).toHaveTextContent("by guardian");
    expect(within(items[1]).getByRole("link", { name: "Transaction" })).toHaveAttribute("href", "/txs/ABC123");

    expect(items[2]).toHaveTextContent("Emergency: closed channel-3");
    expect(within(items[2]).getByRole("link", { name: "Block 150" })).toHaveAttribute("href", "/blocks/150");
  });

  it("opens a row's details with the renderer's body", async () => {
    renderWithProviders(<EmergencyPage />, { api: api(clear as Emergency, rows) });
    fireEvent.click(await screen.findByTestId("emergency-tab-history"));
    const items = await screen.findAllByTestId("emergency-action");
    fireEvent.click(within(items[1]).getByRole("button", { name: "Details" }));
    expect(await within(items[1]).findByTestId("emergency-badge")).toBeInTheDocument();
    expect(within(items[1]).getByTestId("nested-message")).toHaveTextContent("Tokens halted");
  });

  it("tells how a nested guardian action reached the chain: via authz by the grantee, or via a DAO", async () => {
    const nested: EmergencyAction[] = [
      { ...rows[1], height: 230, via: { kind: "authz", grantee: "bze1hotkey00000000000000000000000000000000" } },
      { ...rows[1], height: 220, via: { kind: "dao", dao_id: 3, proposal_id: 9 } },
      { ...rows[1], height: 210, via: { kind: "dao", dao_id: 3, proposal_id: null } },
    ];
    renderWithProviders(<EmergencyPage />, { api: api(clear as Emergency, nested) });
    fireEvent.click(await screen.findByTestId("emergency-tab-history"));
    const items = await screen.findAllByTestId("emergency-action");
    expect(items).toHaveLength(3);

    const authz = within(items[0]).getByTestId("emergency-via");
    expect(authz).toHaveTextContent(/^via authz by\s*bze1hot…0000$/);
    expect(within(authz).getByRole("link")).toHaveAttribute("href", "/accounts/bze1hotkey00000000000000000000000000000000");
    expect(within(items[0]).getByTestId("emergency-by")).toHaveTextContent("by guardian");

    const dao = within(items[1]).getByTestId("emergency-via");
    expect(dao).toHaveTextContent(/^via\s*DAO #3\s*·\s*proposal #9$/);
    expect(within(dao).getByRole("link", { name: "DAO #3" })).toHaveAttribute("href", "/daos/3");
    expect(within(dao).getByRole("link", { name: "proposal #9" })).toHaveAttribute("href", "/daos/3/proposals/9");

    const admin = within(items[2]).getByTestId("emergency-via");
    expect(admin).toHaveTextContent(/^via\s*DAO #3$/);
    expect(within(admin).queryByRole("link", { name: /proposal/ })).toBeNull();
  });

  it("keeps a DAO row whose DAO is not known, linking no DAO or proposal", async () => {
    const unknown: EmergencyAction[] = [{ ...rows[1], height: 220, via: { kind: "dao", dao_id: null, proposal_id: 9 } }];
    renderWithProviders(<EmergencyPage />, { api: api(clear as Emergency, unknown) });
    fireEvent.click(await screen.findByTestId("emergency-tab-history"));
    const items = await screen.findAllByTestId("emergency-action");
    expect(items).toHaveLength(1);
    const via = within(items[0]).getByTestId("emergency-via");
    expect(via).toHaveTextContent(/^via a DAO$/);
    expect(via).not.toHaveTextContent("null");
    expect(within(via).queryByRole("link")).toBeNull();
  });

  it("shows no via line for a direct action", async () => {
    renderWithProviders(<EmergencyPage />, { api: api(clear as Emergency, rows) });
    fireEvent.click(await screen.findByTestId("emergency-tab-history"));
    await screen.findAllByTestId("emergency-action");
    expect(screen.queryByTestId("emergency-via")).toBeNull();
  });

  it("says so when nothing was ever switched", async () => {
    renderWithProviders(<EmergencyPage />, { api: api(clear as Emergency, []) });
    fireEvent.click(await screen.findByTestId("emergency-tab-history"));
    expect(await screen.findByTestId("emergency-history-empty")).toBeInTheDocument();
  });
});
