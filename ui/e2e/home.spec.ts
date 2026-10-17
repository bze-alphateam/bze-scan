import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";
import { mockApi, respond } from "./mock-api";

import sendTx from "../test/fixtures/api/v1/txs/3F9A6B2C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B4C5D0C21E.json";

// The mock API answers /stats, /blocks, /txs and /search from their fixtures
// (query strings ignored): search.json matches Honeycomb and an account.
const HONEYCOMB = "bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c";

test.describe("home page", () => {
  test("renders both lists and no figure tiles; the strip carries the figures", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveTitle("BZE Explorer — blocks, transactions and accounts of the BeeZee network, explained in plain terms");
    await expect(page.getByRole("main").getByRole("heading", { level: 2 })).toHaveText(["Latest blocks", "Latest transactions", "Go to"]);
    await expect(page.getByTestId("tile")).toHaveCount(0);
    await expect(page.getByRole("complementary", { name: "Network summary" }).getByText("25,120,450")).toBeVisible();

    await expect(page.getByTestId("latest-block")).toHaveCount(5);
    await expect(page.getByTestId("latest-block").first().getByRole("link", { name: "25,120,449" })).toBeVisible();
    await expect(page.getByTestId("latest-tx")).toHaveCount(5);
    await expect(page.getByTestId("latest-tx-what").first()).toHaveText("Send 25 BZE to bze1m3k…9ds4");
    await expect(page.getByTestId("latest-tx-parties").nth(1)).toContainText("Honeycomb");
    await expect(page.getByRole("navigation", { name: "BZE apps" }).getByRole("link")).toHaveCount(7);
    await noHorizontalScroll(page);
  });

  test("shows each app's icon on its Go to tile, for the colour mode", async ({ page }) => {
    await page.goto("/");
    const goTo = page.getByRole("navigation", { name: "BZE apps" });
    // Dark is the default theme: the five app icons show their on-dark small cut.
    const apps = goTo.locator("span[data-testid=app-icon] img:visible");
    await expect(apps).toHaveCount(5);
    for (const img of await apps.all()) {
      await expect(img).toHaveAttribute("src", /^\/images\/apps\/[a-z]+\/icon-small-on-dark\.svg$/);
      await expect.poll(() => img.evaluate((i: HTMLImageElement) => i.complete && i.naturalWidth > 0)).toBe(true);
    }
    // Decorative: the tiles keep their plain names, never cut short by the icon.
    await expect(goTo.getByRole("link", { name: "Communities" })).toBeVisible();
    const cut = await goTo.getByRole("link").evaluateAll((links) =>
      links.flatMap((a) => Array.from(a.querySelectorAll<HTMLElement>("[data-testid=go-to-name]")).filter((s) => s.scrollWidth > s.clientWidth).map((s) => s.textContent)),
    );
    expect(cut).toEqual([]);

    // Light mode swaps to the files drawn for a light background.
    await page.addInitScript(() => localStorage.setItem("theme", "light"));
    await page.reload();
    await expect(apps).toHaveCount(5);
    await expect(apps.first()).toHaveAttribute("src", "/images/apps/staking/icon-small.svg");
  });

  test("keeps the lists when the stats route fails", async ({ page }) => {
    await mockApi(page, { "v1/stats": respond(500, { error: { code: "internal", message: "boom" } }) });
    await page.goto("/");
    await expect(page.getByTestId("latest-block")).toHaveCount(5);
    await expect(page.getByTestId("latest-tx")).toHaveCount(5);
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
    await expect(page.getByTestId("search-count")).toHaveText("2 matches · validators first");
    await expect(page.getByTestId("search-result").first()).toHaveAttribute("href", `/validators/${HONEYCOMB}`);
    await expect(page.getByTestId("search-result").first().getByText("validator", { exact: true })).toBeVisible();

    await page.goto("/search?q=h");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Search");
    await expect(page.getByTestId("search-state")).toContainText("Type a little more");

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
