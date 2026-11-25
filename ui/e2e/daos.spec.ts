import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";
import { mockApi, respond } from "./mock-api";

// The mock API answers /daos from test/fixtures/api/v1/daos.json (the query
// is ignored), a DAO from daos/<id>.json and its changes from
// daos/<id>/history.json.

test.describe("DAOs list", () => {
  test("lists the DAOs and opens one", async ({ page, isMobile }) => {
    await page.goto("/daos");
    await expect(page).toHaveTitle("DAOs · BZE Explorer");
    // The nav row is the desktop layout; phones reach the entry through the drawer.
    if (!isMobile) await expect(page.getByRole("navigation", { name: "Explorer" }).getByRole("link", { name: "DAOs" })).toHaveAttribute("href", "/daos");
    const rows = page.getByTestId("dao-row");
    await expect(rows).toHaveCount(2);
    await expect(page.getByTestId("dao-app-link")).toHaveAttribute("href", /^https:\/\/(testnet-)?dao\.getbze\.com$/);
    await noHorizontalScroll(page);
    await rows.first().getByRole("link", { name: "Hive Stakers" }).click();
    await expect(page).toHaveURL(/\/daos\/2$/);
    await expect(page.getByRole("heading", { level: 1, name: "Hive Stakers" })).toBeVisible();
  });

  test("says DAOs arrive with v8.3.0 and hides the nav entry before the upgrade", async ({ page }) => {
    await mockApi(page, { "v1/daos": { items: [], next_cursor: null, supported: false } });
    await page.goto("/daos");
    await expect(page.getByTestId("daos-unsupported")).toContainText("DAOs arrive with chain v8.3.0");
    await expect(page.getByRole("navigation", { name: "Explorer" }).getByRole("link", { name: "DAOs" })).toHaveCount(0);
  });

  test("explains how to create a DAO when there is none", async ({ page }) => {
    await mockApi(page, { "v1/daos": { items: [], next_cursor: null, supported: true } });
    await page.goto("/daos");
    await expect(page.getByTestId("daos-empty").getByRole("link")).toHaveAttribute("href", /\/create$/);
  });
});

test.describe("DAO page", () => {
  test("a member-list DAO shows its tiles and every tab", async ({ page }) => {
    await page.goto("/daos/1");
    await expect(page).toHaveTitle("DAO #1 · Honey Council · BZE Explorer");
    await expect(page.getByTestId("dao-tiles").getByTestId("tile")).toHaveCount(4);
    await expect(page.getByTestId("dao-app-link")).toHaveAttribute("href", /\/dao\/1$/);
    await expect(page.getByTestId("token-row")).toHaveCount(2);

    await page.getByRole("tab", { name: /^Members/ }).click();
    await expect(page.getByTestId("dao-member")).toHaveCount(3);
    await page.getByRole("tab", { name: /^Proposals/ }).click();
    await expect(page.getByTestId("dao-proposals-slot")).toBeVisible();
    await page.getByRole("tab", { name: /^Rules/ }).click();
    await expect(page.getByTestId("dao-rules")).toContainText("40% of the voting power must vote");
    await page.getByRole("tab", { name: /^History/ }).click();
    await expect(page.getByTestId("dao-history-row")).toHaveCount(3);
    await expect(page).toHaveURL(/\/daos\/1\?tab=history$/);
    await noHorizontalScroll(page);
  });

  test("a stake DAO run by another DAO shows the admin chain and its electorate", async ({ page }) => {
    await page.goto("/daos/2?tab=members");
    await expect(page.getByTestId("dao-admin-chain")).toContainText("DAO #1 · Honey Council");
    await expect(page.getByTestId("dao-handover")).toBeVisible();
    await expect(page.getByTestId("dao-source")).toHaveCount(2);
    await expect(page.getByTestId("electorate-explainer")).toContainText("counted only if it joined before the proposal");
    await noHorizontalScroll(page);
  });

  test("an unknown DAO is a not-found page", async ({ page }) => {
    await mockApi(page, { "v1/daos/99": respond(404, { error: { code: "not_found", message: "no" } }) });
    await page.goto("/daos/99");
    await expect(page.getByTestId("dao-not-found")).toBeVisible();
  });
});
