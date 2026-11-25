import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Token } from "@/api/types";
import { fakeApi, renderWithProviders } from "@/test/render";

import honeyJson from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney.json";
import lockedJson from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/ulocked.json";
import plainJson from "../../../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uplain.json";

import { TokenPage } from "./token-page";

const honey = honeyJson as Token;
const locked = lockedJson as Token;
const plain = plainJson as Token;

function show(t: Token) {
  renderWithProviders(<TokenPage denom={t.denom} />, {
    api: fakeApi({
      token: () => Promise.resolve(t),
      tokenHolders: () => Promise.resolve({ items: [], next_cursor: null }),
      tokenTransfers: () => Promise.resolve({ items: [], next_cursor: null }),
    }),
  });
}

describe("factory token settings", () => {
  it("says a token whose minting is off is fixed, and who still manages it", async () => {
    show(locked);
    const box = await screen.findByTestId("supply-box");
    expect(box).toHaveAttribute("data-state", "locked");
    expect(box).toHaveAttribute("data-tone", "ok");
    expect(box).toHaveTextContent("Supply is fixed: minting is turned off for good.");
    expect(box).toHaveTextContent("still manages the name, logo and burns");
    expect(screen.getAllByTestId("tile")[0]).toHaveTextContent("can only go down");
  });

  it("keeps today's states when mintable is true or unknown", async () => {
    show({ ...honey, mintable: true });
    expect(await screen.findByTestId("supply-box")).toHaveAttribute("data-state", "admin");
  });

  it("keeps a renounced token fixed even with minting off", async () => {
    show({ ...locked, admin: null });
    expect(await screen.findByTestId("supply-box")).toHaveAttribute("data-state", "fixed");
  });

  it("words each fee and where it goes, with the raw values in the pop-up", async () => {
    show(locked);
    expect(await screen.findByTestId("settings-gas")).toHaveTextContent("Paying fees in LOCK costs 50% more gas; the extra goes to");
    expect(screen.getByTestId("settings-transfer")).toHaveTextContent("Fees for BZE features paid in LOCK carry a 1% transfer fee, which goes to");
    expect(screen.getByTestId("settings-pool")).toHaveTextContent("The community share of pool trading fees paid in LOCK goes to");
    await userEvent.click(screen.getByTestId("settings-raw-trigger"));
    expect(await screen.findByTestId("settings-raw")).toHaveTextContent("gas_surcharge: 0.500000000000000000 (50%)");
  });

  it("says the extra is burned without a community pool", async () => {
    show({ ...locked, config: { ...locked.config!, community_pool: null, community_pool_label: null } });
    expect(await screen.findByTestId("settings-gas")).toHaveTextContent("the extra is burned.");
    expect(screen.getByTestId("settings-transfer")).toHaveTextContent("which is burned.");
    expect(screen.queryByTestId("settings-pool")).not.toBeInTheDocument();
  });

  it("says there are no special fees for an all-zero config", async () => {
    show(plain);
    expect(await screen.findByTestId("settings-none")).toHaveTextContent("No special fees");
  });

  it("shows no settings card for an unknown config (a v8.2 node) or another kind", async () => {
    show(honey);
    await screen.findByTestId("supply-box");
    expect(screen.queryByTestId("token-settings")).not.toBeInTheDocument();
  });

  it("reads the new event kinds", async () => {
    show({
      ...locked,
      events: [
        { ...locked.events[0], seq: 1, kind: "admin_set_by_governance", details: { from: "", to: "bze1dao" } },
        { ...locked.events[0], seq: 2, kind: "config_changed", details: { community_pool: "bze1pool", transfer_fee: "0.01", gas_surcharge: "0.5" } },
        locked.events[0],
      ],
    });
    await screen.findByTestId("supply-box");
    const lines = (await screen.findAllByTestId("activity-item")).map((e) => e.textContent);
    expect(lines.join("|")).toContain("Governance gave control to");
    expect(lines.join("|")).toContain("Settings changed: extra gas 50%, transfer fee 1%, community pool set");
    expect(lines.join("|")).toContain("Minting turned off for good");
  });
});
