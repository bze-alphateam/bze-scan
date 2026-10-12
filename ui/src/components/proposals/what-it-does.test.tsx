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
    expect(screen.getByRole("link", { name: "see all DEX parameters" })).toHaveAttribute("href", "/parameters#dex");
  });
});
