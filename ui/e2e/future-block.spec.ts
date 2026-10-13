import { expect, test, type Page } from "@playwright/test";

import { noHorizontalScroll } from "./layout";
import { mockApi } from "./mock-api";

import stats from "../test/fixtures/api/v1/stats.json";

// The stats' latest block (25,120,450) is moved to "now" so the extrapolated
// tip stays put; the mock API serves proposal 48, a vote on v8.2.0 at
// 25,200,000, and 404s the other proposals' details.
async function freshStats(page: Page) {
  await mockApi(page, { "v1/stats": { ...stats, latest_time: new Date().toISOString() } });
}

test.describe("future block", () => {
  test("counts down to a height from the query and lists the planned upgrade", async ({ page }) => {
    await freshStats(page);
    await page.goto("/blocks/future?height=25200000");
    await expect(page).toHaveTitle("Future block · BZE Explorer");
    await expect(page.getByRole("heading", { level: 1, name: "Future block" })).toBeVisible();
    await expect(page.getByRole("textbox", { name: "Block height" })).toHaveValue("25,200,000");

    const countdown = page.getByTestId("future-countdown");
    await expect(countdown).toContainText("Block 25,200,000 in about");
    await expect(page.getByTestId("future-when")).toContainText(/Around \w{3} \d{1,2}, \d{4}, \d{2}:\d{2} UTC/);
    const seconds = page.getByTestId("countdown-seconds");
    const first = await seconds.textContent();
    await expect(seconds).not.toHaveText(first ?? "", { timeout: 3000 });

    const planned = page.getByTestId("planned-row");
    await expect(planned).toHaveCount(1);
    await expect(planned.getByRole("link", { name: "Upgrade to v8.2.0" })).toHaveAttribute("href", "/proposals/48");
    await expect(page.getByTestId("window-row")).toHaveCount(4);
    await noHorizontalScroll(page);
  });

  test("estimates a date's height and counts down to it", async ({ page }) => {
    await freshStats(page);
    await page.goto("/blocks/future");
    await page.getByRole("tab", { name: "By date" }).click();
    await page.getByRole("textbox", { name: "Date, day.month.year" }).fill("31.02.2099");
    await expect(page.getByTestId("future-message")).toContainText("Type the date as day.month.year");
    await page.getByRole("textbox", { name: "Date, day.month.year" }).fill("01.01.2099");
    await page.getByRole("textbox", { name: "Time, 24-hour" }).fill("12:00");
    const height = page.getByTestId("future-height");
    await expect(height).toHaveText(/^\d{1,3}(,\d{3})+$/);
    const text = (await height.textContent()) ?? "";
    await noHorizontalScroll(page);

    await page.getByRole("button", { name: `Count down to block ${text}` }).click();
    await expect(page.getByRole("tab", { name: "By block height" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByTestId("future-countdown")).toContainText(`Block ${text} in about`);
  });

  test("is linked from the Blocks page and the footer", async ({ page }) => {
    await page.goto("/blocks");
    await page.getByRole("main").getByRole("link", { name: "Future block" }).click();
    await expect(page).toHaveURL(/\/blocks\/future$/);
    await expect(page.getByRole("contentinfo").getByRole("link", { name: "Future block" })).toHaveAttribute("href", "/blocks/future");
  });

  test("shows in place on a block not produced yet", async ({ page }) => {
    await freshStats(page);
    await page.goto("/blocks/25200000");
    await expect(page.getByRole("heading", { level: 1, name: "Block 25,200,000" })).toBeVisible();
    await expect(page.getByTestId("block-not_produced")).toContainText("Not produced yet");
    await expect(page.getByRole("textbox", { name: "Block height" })).toHaveValue("25,200,000");
    await expect(page.getByTestId("future-countdown")).toContainText("Block 25,200,000 in about");
    await noHorizontalScroll(page);
  });
});
