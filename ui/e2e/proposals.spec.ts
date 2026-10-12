import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

// The mock API answers /proposals from test/fixtures/api/v1/proposals.json
// whatever the status (the query is ignored, so the page's own status check
// keeps the chips honest) and a proposal's routes from proposals/<id>.json and
// proposals/<id>/{votes,deposits}.json. The fixture dates of the voting
// proposal lie in 2099, so it stays open whenever the suite runs.

test.describe("proposals list", () => {
  test("puts the live vote on top and lists every proposal", async ({ page }) => {
    await page.goto("/proposals");
    await expect(page).toHaveTitle("Proposals · BZE Explorer");
    const card = page.getByTestId("voting-card");
    await expect(card).toHaveCount(1);
    await expect(card).toContainText("#48 · Software upgrade");
    await expect(card.getByTestId("tally-row")).toHaveCount(4);
    await expect(card.getByTestId("verdict")).toHaveText("On track to pass");
    await expect(card).toContainText("quorum of 33.4% reached");

    const rows = page.getByTestId("proposal-row");
    await expect(rows).toHaveCount(6);
    await expect(rows.nth(3)).toContainText("Rejected");
    await expect(page.getByTestId("historical-turnout")).toHaveCount(1);
    await expect(rows.nth(5).getByTestId("historical-turnout")).toBeVisible();

    await page.getByRole("button", { name: "Passed", exact: true }).click();
    await expect(rows).toHaveCount(3);
    await page.getByRole("textbox", { name: "Find a proposal" }).fill("liquidity");
    await expect(rows).toHaveCount(1);
    await rows.first().getByRole("link").click();
    await expect(page).toHaveURL(/\/proposals\/45$/);
    await expect(page.getByRole("heading", { level: 1, name: "Fund Liquidity Pools" })).toBeVisible();
    await noHorizontalScroll(page);
  });
});

test.describe("proposal page", () => {
  test("a proposal being voted on shows the checks, where to vote and who voted", async ({ page }) => {
    await page.goto("/proposals/48");
    await expect(page).toHaveTitle("#48 Upgrade network to v8.2.0 · Voting · BZE Explorer");
    await expect(page.getByTestId("proposal-status")).toHaveText("Voting now");
    await expect(page.getByTestId("action")).toContainText("Upgrades the chain software to v8.2.0 at block 25,200,000");
    const votes = page.getByTestId("votes-card");
    await expect(votes.getByTestId("check")).toHaveCount(3);
    await expect(votes.getByTestId("verdict")).toHaveText("On track to pass");
    await expect(page.getByTestId("want-to-vote").getByRole("link", { name: /Vote on Ping.pub/ })).toHaveAttribute("href", /\/gov\/48$/);

    const who = page.getByTestId("who-voted");
    await expect(who.getByTestId("validator-vote")).toHaveCount(2);
    await expect(who.getByTestId("not-voted")).toContainText("Deep Fields");
    await who.getByRole("tab", { name: "Everyone · 5" }).click();
    await expect(who.getByTestId("vote-row")).toHaveCount(5);
    await noHorizontalScroll(page);
  });

  test("a passed community pool spend shows the funds box and its block", async ({ page }) => {
    await page.goto("/proposals/45");
    await expect(page.getByTestId("proposal-status")).toHaveText("Passed");
    await expect(page.getByTestId("result-line")).toHaveText("Passed Dec 13, 2025 · 100% Yes · 70.4% turnout");
    await expect(page.getByTestId("funds-box")).toContainText("3,000,000 BZE");
    await expect(page.getByTestId("spend-done").getByRole("link", { name: "21,512,345" })).toHaveAttribute("href", "/blocks/21512345");
    await expect(page.getByTestId("votes-card")).toContainText("Final result");
    await expect(page.getByTestId("want-to-vote")).toHaveCount(0);
    await expect(page.getByTestId("votes-summary")).toContainText("1 validator voted Yes");

    await page.getByRole("button", { name: "See every vote" }).click();
    await expect(page.getByTestId("validator-vote")).toHaveCount(1);
    await expect(page.getByTestId("historical-turnout")).toHaveCount(0);
    await expect(page.getByTestId("historical-vote")).toHaveCount(0);
    await expect(page.getByTestId("historical-explainer")).toHaveCount(0);
  });

  test("a proposal tallied before the explorer ran marks its archive values historical", async ({ page }) => {
    await page.goto("/proposals/43");
    await expect(page.getByTestId("result-line")).toHaveText("Passed May 6, 2025 · 100% Yes · 67.3% turnout");
    await expect(page.getByTestId("historical-turnout")).toHaveCount(2);
    await expect(page.getByTestId("historical-explainer")).toBeVisible();

    const who = page.getByTestId("who-voted");
    await who.getByRole("button", { name: "See every vote" }).click();
    await expect(who.getByTestId("validator-vote").getByRole("link", { name: "historical" })).toHaveAttribute("href", "/blocks/17131436");
    await who.getByRole("tab", { name: "Everyone · 27" }).click();
    await expect(who.getByTestId("vote-row")).toHaveCount(3);
    await expect(who.getByTestId("historical-vote")).toHaveCount(3);
    await expect(who.getByTestId("vote-row").filter({ hasText: "ago" })).toHaveCount(0);

    await page.getByRole("button", { name: /More details/ }).click();
    await expect(page.getByTestId("deposits")).toContainText("100,000 BZE in total · historical · by");
    await noHorizontalScroll(page);
  });

  test("an unknown or malformed id answers not found", async ({ page }) => {
    await page.goto("/proposals/999");
    await expect(page.getByRole("heading", { name: "Proposal not found" })).toBeVisible();
    const res = await page.goto("/proposals/abc");
    expect(res?.status()).toBe(404);
  });
});

test.describe("proposals on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("stacks the rows and keeps both pages within the screen", async ({ page }) => {
    await page.goto("/proposals");
    await expect(page.getByTestId("proposal-row")).toHaveCount(6);
    await noHorizontalScroll(page);

    await page.goto("/proposals/48");
    await expect(page.getByTestId("validator-vote")).toHaveCount(2);
    await noHorizontalScroll(page);

    await page.goto("/proposals/43");
    await expect(page.getByTestId("historical-explainer")).toBeVisible();
    await page.getByRole("button", { name: "See every vote" }).click();
    await expect(page.getByTestId("historical-vote")).toHaveCount(1);
    await noHorizontalScroll(page);
  });
});
