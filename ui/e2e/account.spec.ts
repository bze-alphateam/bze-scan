import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";
import { mockApi, respond } from "./mock-api";

import plain from "../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4.json";
import nodeDown from "../test/fixtures/api/v1/accounts/bze1q8f7z6x5c4v3d2n0mlkjhgfdsapzxu4a0ex0rq.json";

// The mock API answers /accounts/<address> from test/fixtures/api/v1/accounts/,
// /tokens from tokens.json and /validators from validators.json; the activity
// and ibc routes from the account's directory; a numbered activity page
// (`?page=`) answers the fixture's rows with the account's activity_count as
// the total (1,530 here: 62 pages).
const ADDR = plain.address;
const DEX = "bze18mhtjwczlzqqgvw84uz8lrdv4hqule3jp8allp";

test.describe("module account", () => {
  test("is designed apart: its owner, holdings, what moves it, and no activity or staking", async ({ page }) => {
    await page.goto(`/accounts/${DEX}`);
    await expect(page.getByRole("heading", { level: 1, name: "DEX" })).toBeVisible();
    await expect(page.getByTestId("kind-chip")).toHaveText("Module account");
    await expect(page.getByTestId("module-owner")).toHaveText(
      "Owned by the chain's DEX module (tradebin). Holds the coins in liquidity pools and open orders.",
    );
    const tiles = page.getByTestId("tile");
    await expect(tiles.nth(0)).toContainText("3 tokens, at DEX prices");
    await expect(tiles.nth(1)).toContainText("7.4M BZE");
    await expect(tiles.nth(1)).toContainText("in pools and open orders");
    await expect(tiles.nth(2)).toContainText("people trade");
    await expect(tiles.nth(3)).toContainText("nobody");
    await expect(page.getByTestId("module-callout")).toContainText("This account belongs to the chain, not to a person");

    await expect(page.getByRole("tab")).toHaveText(["Holdings 3", "Movements"]);
    await expect(page.getByRole("tab", { name: "Holdings 3" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByTestId("token-row")).toHaveCount(3);

    await page.getByRole("tab", { name: "Movements" }).click();
    await expect(page).toHaveURL(/\?tab=movements$/);
    await expect(page.getByTestId("module-movements")).toContainText("Module accounts don't send transactions");
    await noHorizontalScroll(page);
  });

  test("moves to another module account from its menu", async ({ page }) => {
    await page.goto(`/accounts/${DEX}`);
    const menu = page.getByLabel("Other module accounts");
    await expect(menu).toHaveValue(DEX);
    await menu.selectOption({ label: "Burner" });
    await expect(page).toHaveURL(/\/accounts\/bze1v7uw4xhrcv0vk7qp8jf9lu3hm5d8uu5yjp5qun$/);
  });
});

test.describe("account page", () => {
  test("a plain account has no module menu", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}`);
    await expect(page.getByRole("heading", { level: 1, name: "Account" })).toBeVisible();
    await expect(page.getByTestId("kind-chip")).toHaveText("Personal account");
    await expect(page.getByLabel("Other module accounts")).toHaveCount(0);
  });

  test("renders the tiles and the three tabs, keeping the tab in the URL", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}`);
    await expect(page).toHaveTitle("Account bze1m3k…9ds4 · BZE Explorer");
    await expect(page.getByRole("heading", { level: 1, name: "Account" })).toBeVisible();
    await expect(page.getByText("active since Apr 2022 · 1,204 transactions")).toBeVisible();

    const tiles = page.getByTestId("tile");
    await expect(tiles).toHaveCount(4);
    await expect(tiles.nth(0)).toContainText("≈ $1.85");
    await expect(tiles.nth(1)).toContainText("with 3 validators");
    await expect(tiles.nth(3)).toContainText("300 BZE");

    await expect(page.getByRole("tab", { name: "Activity 1,530" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByTestId("activity-row")).toHaveCount(8);

    await page.getByRole("tab", { name: "Tokens 5" }).click();
    await expect(page).toHaveURL(/\?tab=tokens$/);
    await expect(page.getByTestId("token-row")).toHaveCount(5);

    await page.getByRole("tab", { name: "Staking 3 validators" }).click();
    await expect(page).toHaveURL(/\?tab=staking$/);
    const rows = page.getByTestId("delegation-row");
    await expect(rows).toHaveCount(3);
    await expect(rows.nth(0)).toContainText("Honeycomb");
    await expect(rows.nth(0)).toContainText("Active");
    await expect(rows.nth(2)).toContainText("Jailed");
    await expect(page.getByTestId("unbonding-item").first()).toContainText("250 BZE from Thamar Node");
    await noHorizontalScroll(page);
  });

  test("opens the Tokens tab straight from ?tab=tokens", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}?tab=tokens`);
    await expect(page.getByRole("tab", { name: "Tokens 5" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByTestId("tokens-summary")).toHaveText("5 tokens · ≈ $37.35 in total, at DEX prices");
    await expect(page.getByTestId("token-symbol").first()).toHaveText("USDC");

    await page.getByRole("button", { name: "By name" }).click();
    await expect(page.getByTestId("token-symbol").first()).toHaveText("BZE");
    await noHorizontalScroll(page);
  });

  test("explains a node that did not answer", async ({ page }) => {
    await page.goto(`/accounts/${nodeDown.address}`);
    await expect(page.getByTestId("live-unavailable")).toContainText("Live balances are unavailable right now");
    await expect(page.getByRole("tab", { name: "Tokens", exact: true })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Activity 5" })).toBeVisible();
  });

  test("answers not found for something that is not an account address", async ({ page }) => {
    await page.goto("/accounts/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c");
    await expect(page.getByRole("heading", { name: "Nothing here" })).toBeVisible();
  });
});

// The open orders come from the account's open-orders.json: HONEY (2 orders,
// one market), BZE (3 orders, two markets) and VDL (unpriced, held only in
// orders). The page never waits for them.
test.describe("account open orders", () => {
  test("counts them on the tab and expands an asset to its markets with DEX links", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}`);
    await expect(page.getByRole("tab", { name: "Orders 6" })).toBeVisible();
    await page.getByRole("tab", { name: "Orders 6" }).click();
    await expect(page).toHaveURL(/\?tab=orders$/);
    await expect(page.getByTestId("orders-summary")).toHaveText("Locked in 6 open orders · 3 markets");
    await expect(page.getByTestId("order-symbol")).toHaveText(["HONEY", "BZE", "VDL"]);
    await expect(page.getByTestId("order-market-row")).toHaveCount(0);

    await page.getByRole("button", { name: "Show the markets holding BZE" }).click();
    const markets = page.getByRole("list", { name: "Markets holding BZE" }).getByTestId("order-market-row");
    await expect(markets).toHaveCount(2);
    await expect(markets.getByTestId("order-market")).toHaveText(["BZE/USDC", "HONEY/BZE"]);
    const dex = markets.nth(0).getByRole("link", { name: "DEX" }).filter({ visible: true });
    await expect(dex).toHaveAttribute("data-testid", "app-link");
    await expect(dex).toHaveAttribute("href", /\/exchange\/market\?id=ubze%2Fibc%2F6490A7EA/);
    await noHorizontalScroll(page);
  });

  test("opens the Orders tab from a Tokens line", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}?tab=tokens`);
    const line = page.getByTestId("token-in-orders").filter({ hasText: "450 BZE" });
    await expect(line).toHaveText("+ 450 BZE in orders");
    await expect(page.getByTestId("token-row")).toHaveCount(5);
    await line.click();
    await expect(page.getByRole("tab", { name: "Orders 6" })).toHaveAttribute("aria-selected", "true");
    await expect(page).toHaveURL(/\?tab=orders$/);
  });

  test("a failed answer changes only the Orders panel and the Tokens lines", async ({ page }) => {
    await mockApi(page, {
      [`v1/accounts/${ADDR}/open-orders`]: respond(502, { error: { code: "upstream_error", message: "the nodes did not answer" } }),
    });
    await page.goto(`/accounts/${ADDR}?tab=tokens`);
    await expect(page.getByTestId("tokens-summary")).toHaveText("5 tokens · ≈ $37.35 in total, at DEX prices");
    await expect(page.getByTestId("tile").nth(0)).toContainText("≈ $1.85");
    await expect(page.getByTestId("token-in-orders")).toHaveCount(0);
    await page.getByRole("tab", { name: "Orders", exact: true }).click();
    await expect(page.getByTestId("orders-error")).toHaveText("The open orders could not be loaded right now.", { timeout: 10_000 });
  });
});

test.describe("account activity", () => {
  test("renders what happened to the account, row by row", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}`);
    const rows = page.getByTestId("activity-row");
    await expect(rows).toHaveCount(8);
    const what = rows.getByTestId("activity-what");
    await expect(what.nth(0)).toContainText("Sent to");
    await expect(what.nth(1)).toHaveText("Order filled · sold 200 BZE for 2.56 USDC on BZE/USDC");
    await expect(rows.nth(1).getByTestId("activity-when")).toContainText("settled in block 25,120,301, no transaction of yours");
    await expect(what.nth(2)).toContainText("Received from");
    await expect(what.nth(3)).toContainText("Staked with");
    await expect(what.nth(3)).toContainText("Honeycomb");
    await expect(what.nth(4)).toHaveText("Funded the Burner");
    await expect(what.nth(5)).toHaveText("Claimed rewards from 2 validators");
    await expect(what.nth(6)).toContainText("Cross-chain transfer from Noble");
    await expect(rows.nth(6).getByTestId("activity-chip")).toHaveText("Received");
    await expect(rows.nth(7).getByTestId("activity-chip")).toHaveText("Failed");
    await expect(rows.nth(7).getByTestId("activity-failed")).toBeVisible();
    await expect(rows.nth(1).getByTestId("activity-amounts")).toHaveText("+ 2.56 USDC");
  });

  test("pages through numbered pages, keeping the page in the URL", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}`);
    const range = page.getByTestId("activity-range");
    await expect(range).toHaveText("Activity 1–25 of 1,530");
    const nav = page.getByRole("navigation", { name: "Pagination" });

    await nav.getByRole("link", { name: "Page 2" }).click();
    await expect(page).toHaveURL(/\?tab=activity&page=2$/);
    await expect(range).toHaveText("Activity 26–50 of 1,530");
    await expect(nav.getByTestId("pager-current")).toHaveText("2");

    await nav.getByLabel("Page", { exact: true }).fill("999");
    await nav.getByLabel("Page", { exact: true }).press("Enter");
    await expect(page).toHaveURL(/\?tab=activity&page=62$/);
    await expect(range).toHaveText("Activity 1,526–1,530 of 1,530");

    await nav.getByRole("link", { name: "First page" }).click();
    await expect(range).toHaveText("Activity 1–25 of 1,530");
  });

  test("filters by category from page 1, keeping it in the URL", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}?tab=activity&page=3`);
    await expect(page.getByTestId("activity-range")).toHaveText("Activity 51–75 of 1,530");

    await page.getByRole("button", { name: "DEX", exact: true }).click();
    await expect(page).toHaveURL(/\?tab=activity&category=dex$/);
    await expect(page.getByTestId("activity-row")).toHaveCount(1);
    await expect(page.getByTestId("activity-range")).toHaveText("Activity 1–1 of 1");
    await expect(page.getByTestId("activity-what")).toHaveText("Order filled · sold 200 BZE for 2.56 USDC on BZE/USDC");

    await page.getByRole("button", { name: "All", exact: true }).click();
    await expect(page.getByTestId("activity-row")).toHaveCount(8);
    await expect(page.getByTestId("activity-range")).toHaveText("Activity 1–25 of 1,530");
  });

  test("opens a category straight from the URL", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}?tab=activity&category=staking`);
    await expect(page.getByRole("button", { name: "Staking", exact: true })).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByTestId("activity-row")).toHaveCount(2);
  });
});

test.describe("account on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("stacks the rows and keeps every tab within the screen", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}`);
    await expect(page.getByTestId("tile")).toHaveCount(4);
    await expect(page.getByTestId("activity-row")).toHaveCount(8);
    await expect(page.getByTestId("pager")).toBeVisible();
    await noHorizontalScroll(page);

    await page.goto(`/accounts/${ADDR}?tab=tokens`);
    await expect(page.getByTestId("token-row")).toHaveCount(5);
    await noHorizontalScroll(page);

    await page.goto(`/accounts/${ADDR}?tab=staking`);
    await expect(page.getByTestId("delegation-row")).toHaveCount(3);
    await noHorizontalScroll(page);

    await page.goto(`/accounts/${DEX}`);
    await expect(page.getByTestId("token-row")).toHaveCount(3);
    await noHorizontalScroll(page);
  });
});
