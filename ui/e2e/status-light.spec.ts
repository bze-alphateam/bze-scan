import { expect, test, type Page } from "@playwright/test";

import behind from "../test/fixtures/variants/status-behind.json";

import { mockApi, networkError } from "./mock-api";

const light = (page: Page) => page.getByRole("button", { name: /^Explorer health/ });

test.describe("health pill", () => {
  test("is Healthy when the explorer keeps up", async ({ page }) => {
    await page.goto("/");
    await expect(light(page)).toHaveAttribute("data-light", "green");
    await expect(light(page)).toHaveText("Healthy");

    await light(page).click();
    const popover = page.getByRole("dialog", { name: "Explorer health" });
    await expect(popover).toContainText("Live indexer25,120,450");
    await expect(popover).toContainText("History from block24,998,316");
    await expect(popover).toContainText("BackfillRunning");
    await expect(popover).toContainText("The explorer keeps up with the chain.");
  });

  test("is Stalled when the explorer is behind", async ({ page }) => {
    await mockApi(page, { "v1/status": behind });
    await page.goto("/");
    await expect(light(page)).toHaveAttribute("data-light", "red");
    await expect(light(page)).toHaveText("Stalled");

    await light(page).click();
    await expect(page.getByRole("dialog")).toContainText("Live indexer25,120,100 · 351 blocks behind");
    await expect(page.getByRole("dialog")).toContainText("Archive—");
  });

  test("is grey when the status cannot be read", async ({ page }) => {
    await mockApi(page, { "v1/status": networkError });
    await page.goto("/");
    await expect(light(page)).toHaveAttribute("data-light", "unknown");
    await expect(light(page)).toHaveText("Unknown");
  });
});
