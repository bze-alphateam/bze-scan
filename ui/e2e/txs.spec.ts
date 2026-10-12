import { expect, test, type Page } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

import latest from "../test/fixtures/api/v1/txs.json";

// The mock API answers /txs from test/fixtures/api/v1/txs.json whatever the
// query; the routes below answer the Failed chip and the older page.
const OLDER = { items: [{ ...latest.items[9], height: 25120439, hash: "A".repeat(64) }], next_cursor: null };
const FAILED = { items: latest.items.filter((t) => !t.success), next_cursor: null };

async function listRoutes(page: Page) {
  await page.route(/\/api\/v1\/txs\?/, async (route) => {
    const params = new URL(route.request().url()).searchParams;
    const json = params.get("cursor") ? OLDER : params.get("status") === "failed" ? FAILED : null;
    if (!json) return route.fallback();
    return route.fulfill({ json, headers: { "Access-Control-Allow-Origin": "*", "Cache-Control": "no-store" } });
  });
}

const rows = (page: Page) => page.getByTestId("tx-row");

test.describe("transactions list", () => {
  test("lists the latest transactions, filters by Failed and loads an older page", async ({ page }) => {
    await listRoutes(page);
    await page.goto("/txs");
    await expect(page).toHaveTitle("Transactions · BZE Explorer");
    await expect(rows(page)).toHaveCount(10);
    await expect(rows(page).first().getByTestId("tx-what")).toHaveText("Send 25 BZE to bze1m3k…9ds4");
    await expect(rows(page).first().getByTestId("tx-height")).toHaveText("block 25,120,449");

    await page.getByRole("button", { name: "Show older transactions" }).click();
    await expect(rows(page)).toHaveCount(11);
    await expect(page.getByTestId("txs-snapshot")).toBeVisible();
    await page.getByRole("button", { name: "Back to the latest transactions" }).click();
    await expect(rows(page)).toHaveCount(10);

    await page.getByRole("button", { name: "Failed" }).click();
    await expect(rows(page)).toHaveCount(2);
    await expect(rows(page).getByText("Failed", { exact: true })).toHaveCount(2);
    await noHorizontalScroll(page);
  });
});

test.describe("transactions on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("stacks the rows within the screen", async ({ page }) => {
    await page.goto("/txs");
    await expect(rows(page)).toHaveCount(10);
    await noHorizontalScroll(page);
  });
});
