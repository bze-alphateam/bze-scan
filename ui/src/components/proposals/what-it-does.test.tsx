import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Proposal } from "@/api/types";
import { fakeApi, renderWithProviders } from "@/test/render";

import proposal from "../../../test/fixtures/api/v1/proposals/45.json";

import { ActionView } from "./what-it-does";

describe("ActionView", () => {
  it("names a parameter change's keys from the parameters dictionary and links to its group", () => {
    renderWithProviders(
      <ActionView
        a={{
          type: "params",
          module: "tradebin",
          entries: [
            { key: "marketTakerFee", value: { denom: "ubze", amount: "100000" } },
            { key: "native_denom", value: "ubze" },
          ],
          whole: false,
        }}
        p={proposal as Proposal}
        upgradeEta={null}
      />,
      { api: fakeApi() },
    );
    expect(screen.getByText("Changes 2 tradebin parameters:")).toBeInTheDocument();
    expect(screen.getByText("Taker fee")).toBeInTheDocument();
    expect(screen.getByText("0.1 BZE per order")).toBeInTheDocument();
    expect(screen.getByText("Native denom")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "see all DEX parameters" })).toHaveAttribute("href", "/parameters?group=dex");
  });

  it("draws a governance-only message with its renderer's title and body", () => {
    renderWithProviders(
      <ActionView
        a={{
          type: "message",
          typeUrl: "/bze.guardian.v1.MsgReopenChannel",
          body: { "@type": "/bze.guardian.v1.MsgReopenChannel", authority: "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw", channel_id: "channel-3" },
        }}
        p={proposal as Proposal}
        upgradeEta={null}
      />,
      { api: fakeApi() },
    );
    const action = screen.getByTestId("action");
    expect(action).toHaveTextContent("Reopened channel-3");
    expect(action).toHaveTextContent("Decided by");
  });

  it("names the pools of a fee proposal", () => {
    renderWithProviders(
      <ActionView
        a={{
          type: "message",
          typeUrl: "/bze.tradebin.MsgUpdatePools",
          body: { "@type": "/bze.tradebin.MsgUpdatePools", updates: [{ pool_id: "p1", fee: "0.005", fee_dest: { treasury: "1", burner: "0" } }] },
        }}
        p={proposal as Proposal}
        upgradeEta={null}
      />,
      { api: fakeApi() },
    );
    expect(screen.getByText("Changed the fees of 1 pool")).toBeInTheDocument();
    expect(screen.getByText("new fee 0.5% (100% to the treasury, 0% burned)")).toBeInTheDocument();
  });
});
