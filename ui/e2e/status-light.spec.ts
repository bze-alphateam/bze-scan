import { expect, test, type Page } from "@playwright/test";

import behind from "../test/fixtures/variants/status-behind.json";

import { mockApi, networkError } from "./mock-api";

const light = (page: Page) => page.getByRole("button", { name: /^Explorer status/ });

test.describe("status light", () => {
  test("is green when the explorer keeps up", async ({ page }) => {
    await page.goto("/");
    await expect(light(page)).toHaveAttribute("data-light", "green");
    await expect(light(page)).toHaveText("Up to date");

    await light(page).click();
    const popover = page.getByRole("dialog");
    await expect(popover).toContainText("Explorer height25,120,450");
    await expect(popover).toContainText("History from block 24,998,316, older blocks still loading");
  });

  test("is red when the explorer is behind", async ({ page }) => {
    await mockApi(page, { "v1/status": behind });
    await page.goto("/");
    await expect(light(page)).toHaveAttribute("data-light", "red");
    await expect(light(page)).toHaveText("Behind");

    await light(page).click();
    await expect(page.getByRole("dialog")).toContainText("Archive height—");
  });

  test("is grey when the status cannot be read", async ({ page }) => {
    await mockApi(page, { "v1/status": networkError });
    await page.goto("/");
    await expect(light(page)).toHaveAttribute("data-light", "unknown");
    await expect(light(page)).toHaveText("Unknown");
  });
});
