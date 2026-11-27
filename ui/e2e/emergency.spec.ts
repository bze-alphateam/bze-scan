import { expect, test } from "@playwright/test";

import actions from "../test/fixtures/api/v1/emergency/actions.json";
import active from "../test/fixtures/variants/emergency-active.json";
import { noHorizontalScroll } from "./layout";
import { mockApi } from "./mock-api";

test.describe("emergency status", () => {
  test("all clear: the page says so and there is no banner", async ({ page }) => {
    await page.goto("/emergency");
    await expect(page.getByTestId("emergency-status-card")).toContainText("No emergency measures are active");
    await expect(page.getByTestId("emergency-normal")).toContainText("Everything is normal");
    await expect(page.getByTestId("emergency-banner")).toHaveCount(0);
    await noHorizontalScroll(page);
  });

  test("active: every switch is listed", async ({ page }) => {
    await mockApi(page, { "v1/emergency": active });
    await page.goto("/emergency");
    await expect(page.getByTestId("emergency-status-card")).toContainText("6 emergency measures are active");
    await expect(page.getByTestId("emergency-halted")).toContainText("USDC");
    await expect(page.getByTestId("emergency-channels")).toContainText("channel-0 to Osmosis");
    await expect(page.getByTestId("emergency-halt")).toContainText("Chain halt scheduled at block 25,120,900");
    await noHorizontalScroll(page);
  });

  test("a DAO guardian is shown as the DAO", async ({ page }) => {
    await mockApi(page, { "v1/emergency": { ...active, guardian: { address: "bze1dao", label: "DAO #3 · Ops", label_kind: "dao" } } });
    await page.goto("/emergency");
    await expect(page.getByTestId("emergency-status-card").getByRole("link", { name: "DAO #3 · Ops" })).toHaveAttribute("href", "/daos/3");
  });

  test("the banner shows on the home page when active and leads to the page", async ({ page }) => {
    await mockApi(page, { "v1/emergency": active });
    await page.goto("/");
    const banner = page.getByTestId("emergency-banner");
    await expect(banner).toContainText("Emergency measures active: trading halted for 2 tokens");
    await banner.getByRole("link", { name: "details →" }).click();
    await expect(page).toHaveURL(/\/emergency$/);
  });

  test("history lists the actions with who acted", async ({ page }) => {
    await page.goto("/emergency");
    await page.getByTestId("emergency-tab-history").click();
    const rows = page.getByTestId("emergency-action");
    await expect(rows).toHaveCount(2);
    await expect(rows.first().getByRole("link", { name: "Proposal #52" })).toHaveAttribute("href", "/proposals/52");
    await expect(rows.nth(1).getByTestId("emergency-by")).toContainText("by guardian");
  });

  test("history says how a nested guardian action reached the chain", async ({ page }) => {
    const [, direct] = actions.items;
    const nested = {
      items: [
        { ...direct, height: 25120500, action: "close_channel", details: { channel_id: "channel-3" }, via: { kind: "authz", grantee: "bze1hotkey00000000000000000000000000000000" } },
        { ...direct, height: 25120400, via: { kind: "dao", dao_id: 3, proposal_id: 9 } },
        ...actions.items,
      ],
      next_cursor: null,
    };
    await mockApi(page, { "v1/emergency/actions": nested });
    await page.goto("/emergency");
    await page.getByTestId("emergency-tab-history").click();
    const rows = page.getByTestId("emergency-action");
    await expect(rows).toHaveCount(4);
    await expect(rows.first().getByTestId("emergency-via")).toContainText("via authz by");
    await expect(rows.first().getByTestId("emergency-via").getByRole("link")).toHaveAttribute("href", "/accounts/bze1hotkey00000000000000000000000000000000");
    await expect(rows.nth(1).getByTestId("emergency-via").getByRole("link", { name: "DAO #3" })).toHaveAttribute("href", "/daos/3");
    await expect(rows.nth(1).getByTestId("emergency-via").getByRole("link", { name: "proposal #9" })).toHaveAttribute("href", "/daos/3/proposals/9");
    await expect(rows.nth(1).getByTestId("emergency-by")).toContainText("by guardian");
    await expect(rows.nth(2).getByTestId("emergency-via")).toHaveCount(0);
    await noHorizontalScroll(page);
  });

  test("an older node says the switches arrive with v8.3.0", async ({ page }) => {
    await mockApi(page, { "v1/emergency": { ...active, supported: false, active: false } });
    await page.goto("/emergency");
    await expect(page.getByTestId("emergency-unsupported")).toContainText("arrive with chain v8.3.0");
    await expect(page.getByTestId("emergency-banner")).toHaveCount(0);
  });
});
