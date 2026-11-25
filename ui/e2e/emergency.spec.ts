import { expect, test } from "@playwright/test";

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

  test("an older node says the switches arrive with v8.3.0", async ({ page }) => {
    await mockApi(page, { "v1/emergency": { ...active, supported: false, active: false } });
    await page.goto("/emergency");
    await expect(page.getByTestId("emergency-unsupported")).toContainText("arrive with chain v8.3.0");
    await expect(page.getByTestId("emergency-banner")).toHaveCount(0);
  });
});
