import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";
import { mockApi, respond } from "./mock-api";

import stats from "../test/fixtures/api/v1/stats.json";
import sendTx from "../test/fixtures/api/v1/txs/3F9A6B2C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B4C5D0C21E.json";

// The mock API answers /stats, /blocks, /txs and /search from their fixtures
// (query strings ignored): search.json matches Honeycomb and an account.
const HONEYCOMB = "bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c";

const tile = (page: import("@playwright/test").Page, label: string) => page.getByTestId("tile").filter({ hasText: label });

test.describe("home page", () => {
  test("renders the tiles and both lists, and refreshes the tiles", async ({ page }) => {
    let height = stats.latest_height;
    await page.route("**/api/v1/stats", (route) =>
      route.fulfill({
        json: { ...stats, latest_height: height },
        headers: { "Access-Control-Allow-Origin": "*", "Cache-Control": "no-store" },
      }),
    );
    await page.goto("/");
    await expect(page).toHaveTitle("BZE Explorer — blocks, transactions and accounts of the BeeZee network, explained in plain terms");
    await expect(page.getByTestId("tile")).toHaveCount(6);
    await expect(tile(page, "Latest block")).toContainText("25,120,450");
    await expect(tile(page, "BZE price")).toContainText("+1.2% today");

    await expect(page.getByTestId("latest-block")).toHaveCount(5);
    await expect(page.getByTestId("latest-block").first().getByRole("link", { name: "25,120,449" })).toBeVisible();
    await expect(page.getByTestId("latest-tx")).toHaveCount(5);
    await expect(page.getByTestId("latest-tx-what").first()).toHaveText("Send 25 BZE to bze1m3k…9ds4");
    await expect(page.getByTestId("latest-tx-parties").nth(1)).toContainText("Honeycomb");

    height += 3;
    await expect(tile(page, "Latest block")).toContainText("25,120,453", { timeout: 10_000 });
    await noHorizontalScroll(page);
  });

  test("keeps the page when the stats route fails", async ({ page }) => {
    await mockApi(page, { "v1/stats": respond(500, { error: { code: "internal", message: "boom" } }) });
    await page.goto("/");
    await expect(tile(page, "Latest block")).toContainText("—", { timeout: 15_000 });
    await expect(page.getByTestId("latest-block")).toHaveCount(5);
  });
});

test.describe("search", () => {
  test("typing a moniker lists it and Enter opens the validator", async ({ page }) => {
    await page.goto("/");
    const box = page.getByRole("main").getByRole("combobox", { name: "Search the explorer" });
    await box.fill("Honey");
    const options = page.getByRole("main").getByRole("option");
    await expect(options.first()).toContainText("Honeycomb");
    await expect(options.first()).toContainText("validator");
    await expect(options.nth(1)).toContainText("account");
    await box.press("Enter");
    await expect(page).toHaveURL(new RegExp(`/validators/${HONEYCOMB}$`));
  });

  test("pasting a hash opens the transaction page", async ({ page }) => {
    await page.goto("/");
    const box = page.getByRole("main").getByRole("combobox", { name: "Search the explorer" });
    await box.fill(sendTx.hash.toLowerCase());
    await box.press("Enter");
    await expect(page).toHaveURL(new RegExp(`/txs/${sendTx.hash}$`));
    await expect(page.getByTestId("tx-hash")).toHaveText(sendTx.hash);
  });

  test("the search page lists the matches and opens an identifier's page", async ({ page }) => {
    await page.goto("/search?q=hon");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Results for “hon”");
    await expect(page.getByTestId("search-result").first()).toHaveAttribute("href", `/validators/${HONEYCOMB}`);

    await page.goto("/search?q=25120440");
    await expect(page).toHaveURL(/\/blocks\/25120440$/);
  });
});

test.describe("search without JavaScript", () => {
  test.use({ javaScriptEnabled: false });

  test("submits the form to the search page", async ({ page }) => {
    await page.goto("/");
    const box = page.getByRole("main").getByRole("combobox", { name: "Search the explorer" });
    await box.fill("hon");
    await box.press("Enter");
    await expect(page).toHaveURL(/\/search\?q=hon$/);
    await expect(page.getByTestId("search-result")).toHaveCount(2);
  });
});

test.describe("home on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("fits the screen", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByTestId("latest-tx")).toHaveCount(5);
    await noHorizontalScroll(page);
    const box = page.getByRole("main").getByRole("combobox", { name: "Search the explorer" });
    await box.fill("Honey");
    await expect(page.getByRole("main").getByRole("option").first()).toBeVisible();
    await noHorizontalScroll(page);
  });
});
