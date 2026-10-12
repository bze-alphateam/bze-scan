import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

import params from "../test/fixtures/api/v1/params.json";

// The mock API answers /params from test/fixtures/api/v1/params.json (all
// eleven modules), /stats from stats.json and the last change's proposal from
// proposals/44.json.

test.describe("chain parameters", () => {
  test("renders every group from the live values and links the last change", async ({ page }) => {
    await page.goto("/parameters");
    await expect(page).toHaveTitle("Chain parameters · BZE Explorer");
    await expect(page.getByTestId("param-group")).toHaveCount(10);
    await expect(page.getByText("not available")).toHaveCount(0);

    const last = page.getByTestId("last-change-link");
    await expect(last).toHaveText("#44 V8 Upgrade");
    await expect(last).toHaveAttribute("href", "/proposals/44");

    const staking = page.locator("section#staking");
    await expect(staking.getByTestId("param-row").first()).toContainText("Unstaking period");
    await expect(staking.getByTestId("param-row").first()).toContainText("1814400s");
    await expect(page.locator("section#dex").getByRole("link", { name: "Burner module · module" })).toBeVisible();
  });

  test("the anchor nav scrolls to a group", async ({ page }) => {
    await page.goto("/parameters");
    await page.getByTestId("param-nav").getByRole("link", { name: "CoinTrunk" }).click();
    await expect(page).toHaveURL(/#cointrunk$/);
    await expect(page.locator("section#cointrunk")).toBeInViewport();
  });

  test("copies every module's values as JSON", async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.goto("/parameters");
    await page.getByRole("button", { name: "Copy all as JSON" }).click();
    await expect(page.getByRole("button", { name: "Copied" })).toBeVisible();
    const copied = await page.evaluate(() => navigator.clipboard.readText());
    expect(JSON.parse(copied)).toEqual(params);
  });
});

test.describe("chain parameters on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("stacks each row and stays within the screen", async ({ page }) => {
    await page.goto("/parameters");
    await expect(page.getByTestId("param-group")).toHaveCount(10);
    await noHorizontalScroll(page);
  });
});
