import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

import plain from "../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4.json";
import nodeDown from "../test/fixtures/api/v1/accounts/bze1q8f7z6x5c4v3d2n0mlkjhgfdsapzxu4a0ex0rq.json";

// The mock API answers /accounts/<address> from test/fixtures/api/v1/accounts/,
// /tokens from tokens.json and /validators from validators.json.
const ADDR = plain.address;

test.describe("account page", () => {
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

    await expect(page.getByRole("tab", { name: "Activity" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByText("Activity is coming")).toBeVisible();

    await page.getByRole("tab", { name: "Tokens · 5" }).click();
    await expect(page).toHaveURL(/\?tab=tokens$/);
    await expect(page.getByTestId("token-row")).toHaveCount(5);

    await page.getByRole("tab", { name: "Staking · 3 validators" }).click();
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
    await expect(page.getByRole("tab", { name: "Tokens · 5" })).toHaveAttribute("aria-selected", "true");
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
  });

  test("answers not found for something that is not an account address", async ({ page }) => {
    await page.goto("/accounts/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c");
    await expect(page.getByRole("heading", { name: "Nothing here" })).toBeVisible();
  });
});

test.describe("account on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("stacks the rows and keeps every tab within the screen", async ({ page }) => {
    await page.goto(`/accounts/${ADDR}`);
    await expect(page.getByTestId("tile")).toHaveCount(4);
    await noHorizontalScroll(page);

    await page.goto(`/accounts/${ADDR}?tab=tokens`);
    await expect(page.getByTestId("token-row")).toHaveCount(5);
    await noHorizontalScroll(page);

    await page.goto(`/accounts/${ADDR}?tab=staking`);
    await expect(page.getByTestId("delegation-row")).toHaveCount(3);
    await noHorizontalScroll(page);
  });
});
