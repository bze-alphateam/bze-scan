import { expect, test } from "@playwright/test";

import { mockApi } from "./mock-api";

// The chain paused by an emergency halt (chain v8.3.0): the guardian scheduled
// it at the block after the explorer's tip (status.json: 25,120,450), with no
// proposal behind it.
const HEIGHT = 25120451;
const emergency = {
  halted: true,
  height: HEIGHT,
  halt_height: HEIGHT + 1,
  proposal_id: null,
  name: `emergency-halt-${HEIGHT}`,
  kind: "emergency",
  reason: "pool drain under way",
  scheduled_by: "bze1guardian",
  round: 1,
  halted_since: new Date(Date.now() - 10 * 60_000).toISOString(),
  checked_at: new Date().toISOString(),
  total_power: "1000",
  back_power: "400",
  prevote_power: "400",
  precommit_power: "0",
  members: [
    { consensus_address: "AAAA", operator_address: null, moniker: null, voting_power: "400", back: true, prevote: true, precommit: false },
    { consensus_address: "BBBB", operator_address: null, moniker: null, voting_power: "600", back: false, prevote: false, precommit: false },
  ],
};

test.describe("emergency halt", () => {
  test("the banner names it and leads to its block, which shows the halt", async ({ page }) => {
    await mockApi(page, { "v1/upgrades/live": emergency });
    await page.goto("/");
    const banner = page.getByTestId("halt-banner");
    await expect(banner).toContainText("Emergency halt scheduled by the guardian at block 25,120,451: pool drain under way.");
    await expect(banner).toContainText("40.0% of the voting power is back");

    await banner.getByRole("link", { name: "See who is missing" }).click();
    await expect(page).toHaveURL(new RegExp(`/blocks/${HEIGHT}$`));
    const note = page.getByTestId("emergency-halt");
    await expect(note.getByTestId("emergency-halt-line")).toHaveText(
      "Emergency halt scheduled by the guardian at block 25,120,451: pool drain under way",
    );
    await expect(note.getByTestId("live-halt")).toContainText("Chain paused by an emergency halt");
    await expect(note.getByRole("list", { name: "Validators not back yet" }).getByRole("listitem")).toHaveCount(1);
  });

  test("the future blocks page lists it", async ({ page }) => {
    await mockApi(page, { "v1/upgrades/live": emergency });
    await page.goto("/blocks/future");
    const row = page.getByTestId("planned-emergency");
    await expect(row).toContainText("at block 25,120,451 · scheduled by the guardian: pool drain under way · the chain is paused");
    await expect(row.getByRole("link", { name: "Emergency halt" })).toHaveAttribute("href", `/blocks/${HEIGHT}`);
  });
});
