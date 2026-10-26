import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";
import { mockApi } from "./mock-api";

import params from "../test/fixtures/api/v1/params.json";

// The mock API answers /params from test/fixtures/api/v1/params.json (all
// eleven modules), /params/halted-denoms from params/halted-denoms.json (ATOM
// and a denom the tokens list does not know), /stats from stats.json and
// /modules from modules.json.

test.describe("chain parameters", () => {
  test("shows Fees first, one group at a time, with its module accounts", async ({ page }) => {
    await page.goto("/parameters");
    await expect(page).toHaveTitle("Chain parameters · BZE Explorer");
    const group = page.getByTestId("param-group");
    await expect(group).toHaveCount(1);
    await expect(group).toHaveAttribute("id", "fees");
    await expect(page.getByText("not available")).toHaveCount(0);
    await expect(page.getByTestId("module-account-row")).toHaveCount(4);
    await expect(page.getByTestId("module-account-row").first()).toContainText("Fee collector");
  });

  test("the group nav switches the group and keeps it in the URL", async ({ page }) => {
    await page.goto("/parameters");
    await page.getByTestId("param-nav").getByRole("link", { name: /^Staking/ }).click();
    await expect(page).toHaveURL(/\/parameters\?group=staking$/);
    await expect(page.getByTestId("param-group")).toHaveAttribute("id", "staking");

    const unbonding = page.getByTestId("param-row").first();
    await expect(unbonding).toContainText("Unstaking period");
    await unbonding.getByRole("button", { name: "Info about Unstaking period" }).click();
    await expect(unbonding.getByTestId("param-info")).toContainText("1814400s");
    await expect(unbonding.getByTestId("param-info")).toContainText("unbonding_time");
  });

  test("opens the group of ?group= and of a legacy #anchor", async ({ page }) => {
    await page.goto("/parameters?group=cointrunk");
    await expect(page.getByTestId("param-group")).toHaveAttribute("id", "cointrunk");
    await expect(page.getByTestId("module-accounts")).toContainText("CoinTrunk has no module account");

    await page.goto("/parameters#dex");
    await expect(page.getByTestId("param-group")).toHaveAttribute("id", "dex");
    await expect(page).toHaveURL(/\/parameters\?group=dex$/);
    await expect(page.getByRole("link", { name: "Burner module · module" })).toBeVisible();
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

test.describe("halted denoms", () => {
  test("are a group of their own after DEX, each token named over its denom", async ({ page }) => {
    await page.goto("/parameters?group=dex");
    await page.getByTestId("param-nav").getByRole("link", { name: /^Halted denoms/ }).click();
    await expect(page).toHaveURL(/\/parameters\?group=halted$/);
    const section = page.getByTestId("halted-denoms");
    await expect(section).toContainText("no trading, swaps, new pools or fee payment");
    const rows = section.getByTestId("halted-denom");
    await expect(rows).toHaveCount(2);
    await expect(rows.first().getByRole("link", { name: "ATOM" })).toBeVisible();
    await expect(rows.first()).toContainText("Cosmos Hub Atom");
    await expect(rows.first().getByTestId("halted-denom-raw")).toHaveText(/^ibc\/27394FB0/);
    await expect(rows.nth(1).getByRole("link", { name: "Unknown token" })).toBeVisible();
    await noHorizontalScroll(page);
  });

  test("say so when none is halted", async ({ page }) => {
    await mockApi(page, { "v1/params/halted-denoms": { items: [] } });
    await page.goto("/parameters?group=halted");
    await expect(page.getByTestId("halted-empty")).toContainText("No denom is halted");
  });
});

test.describe("chain parameters on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("shows the groups as chips and stays within the screen", async ({ page }) => {
    await page.goto("/parameters?group=burner");
    await expect(page.getByTestId("param-group")).toHaveAttribute("id", "burner");
    await expect(page.getByTestId("param-nav-item")).toHaveCount(11);
    // Below c3 the short form shows; the full address stays hidden (and in the title).
    const address = page.getByTestId("module-account-address").first();
    await expect(address.getByText("bze1v7u…5qun")).toBeVisible();
    await expect(address.getByText("bze1v7uw4xhrcv0vk7qp8jf9lu3hm5d8uu5yjp5qun")).toBeHidden();
    await noHorizontalScroll(page);
  });
});
