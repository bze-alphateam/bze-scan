import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";
import { mockApi } from "./mock-api";

const usdc = "ibc%2F6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4";

test.describe("cross-chain", () => {
  test("lists the channels with their limits", async ({ page }) => {
    await page.goto("/ibc");
    await expect(page).toHaveTitle("Cross-chain · BZE Explorer");
    await expect(page.getByTestId("channel-card")).toHaveCount(8);
    const osmosis = page.getByTestId("channel-card").first();
    await expect(osmosis).toContainText("Osmosis");
    await expect(osmosis.getByTestId("limit-out")).toContainText("1.2M of 2M BZE used (60%)");
    await expect(osmosis.getByTestId("limit-reset")).toContainText("resets in");
    await expect(page.getByTestId("blocked-denoms")).toBeVisible();
    await page.getByTestId("limits-help").click();
    await expect(page.getByRole("dialog")).toContainText("net flow (out minus in)");
    await page.keyboard.press("Escape");
    await noHorizontalScroll(page);
  });

  test("a long unknown blocked denom does not widen the page", async ({ page }) => {
    const long = `factory/bze1${"q".repeat(38)}/${"u".repeat(40)}`;
    await mockApi(page, { "v1/ibc/rate-limits": { supported: true, items: [], blacklisted_denoms: [long] } });
    await page.goto("/ibc");
    await expect(page.getByTestId("blocked-denoms")).toContainText(long);
    await noHorizontalScroll(page);
  });

  test("lists the channels without limits before chain v8.3.0", async ({ page }) => {
    await mockApi(page, { "v1/ibc/rate-limits": { supported: false, items: [], blacklisted_denoms: [] } });
    await page.goto("/ibc");
    await expect(page.getByTestId("channel-card")).toHaveCount(8);
    await expect(page.getByTestId("limits-unsupported")).toContainText("IBC limits arrive with chain v8.3.0");
    await expect(page.getByTestId("limit-row")).toHaveCount(0);
  });

  test("a token page shows its limits and links to the page", async ({ page }) => {
    await page.goto(`/tokens/${usdc}`);
    await expect(page.getByTestId("token-limits")).toContainText("Cross-chain limits");
    await expect(page.getByTestId("token-limits").getByTestId("limit-in")).toContainText("not allowed on this route");
    await noHorizontalScroll(page);
    await page.getByRole("link", { name: /All cross-chain limits/ }).click();
    await expect(page).toHaveURL(/\/ibc$/);
  });

  test("the main nav reaches it", async ({ page, isMobile }) => {
    await page.goto("/");
    test.skip(isMobile, "the phone menu is covered by the shell spec");
    await page.getByLabel("Explorer").getByRole("link", { name: "Cross-chain" }).click();
    await expect(page).toHaveURL(/\/ibc$/);
  });
});
