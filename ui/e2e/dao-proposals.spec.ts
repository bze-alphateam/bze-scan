import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";
import { mockApi, respond } from "./mock-api";

// The mock API answers a DAO's proposals from daos/<id>/proposals.json (the
// status filter is ignored), a proposal from dao-proposals/<id>.json and its
// votes from dao-proposals/<id>/votes.json. The fixture dates of the voting
// proposal lie in 2099, so it stays open whenever the suite runs.

test.describe("DAO proposals tab", () => {
  test("lists the proposals with the live vote bar and opens one", async ({ page }) => {
    await page.goto("/daos/2?tab=proposals");
    const rows = page.getByTestId("dao-proposal-row");
    await expect(rows).toHaveCount(3);
    await expect(rows.nth(0).getByTestId("dao-proposal-status")).toHaveText("Voting");
    await expect(rows.nth(0).getByTestId("dao-tally-row")).toHaveCount(3);
    await expect(rows.nth(1).getByTestId("dao-proposal-status")).toHaveText("Passed (executed)");
    await expect(rows.nth(1).getByTestId("dao-tally-row")).toHaveCount(0);
    await expect(rows.nth(2).getByTestId("dao-proposal-status")).toHaveText("Failed to execute");
    await noHorizontalScroll(page);

    await page.getByRole("button", { name: "Failed to execute", exact: true }).click();
    await expect(page.getByRole("button", { name: "Failed to execute", exact: true })).toHaveAttribute("aria-pressed", "true");

    await rows.nth(0).getByRole("link", { name: /Fund the hive festival/ }).click();
    await expect(page).toHaveURL(/\/daos\/2\/proposals\/7$/);
    await expect(page.getByRole("heading", { level: 1, name: "Fund the hive festival" })).toBeVisible();
  });

  test("says a DAO without proposals has none", async ({ page }) => {
    await mockApi(page, { "v1/daos/2/proposals": { items: [], next_cursor: null } });
    await page.goto("/daos/2?tab=proposals");
    await expect(page.getByTestId("dao-proposals-empty")).toContainText("no proposals yet");
  });
});

test.describe("DAO proposal page", () => {
  test("a voting expedited proposal shows tiles, checks, what it does and the votes", async ({ page }) => {
    await page.goto("/daos/2/proposals/7");
    await expect(page).toHaveTitle(/^#7 Fund the hive festival · Voting · BZE Explorer$/);
    await expect(page.getByTestId("dao-proposal-status")).toHaveText("Voting");
    await expect(page.getByTestId("expedited-tag")).toBeVisible();
    await expect(page.getByTestId("dao-chip")).toHaveAttribute("href", "/daos/2");
    await expect(page.locator('[data-testid="dao-proposal-tiles"] > *')).toHaveCount(5);
    await expect(page.getByTestId("tile-turnout")).toContainText("52.0%");
    await expect(page.getByTestId("checks-regular").getByTestId("check")).toHaveCount(2);
    await expect(page.getByTestId("checks-expedite").getByTestId("check")).toHaveCount(2);
    await expect(page.getByTestId("dao-action")).toContainText("Send 250 BZE");
    await expect(page.getByTestId("runs-as-dao")).toContainText("treasury account");
    await noHorizontalScroll(page);

    await page.getByRole("button", { name: "From the proposer" }).click();
    await expect(page.getByTestId("dao-summary-text")).toContainText("<b>this is not HTML</b>");

    await page.getByRole("tab", { name: /^Votes/ }).click();
    await expect(page).toHaveURL(/\?tab=votes$/);
    await expect(page.getByTestId("dao-vote-row")).toHaveCount(4);
    await expect(page.getByTestId("dao-votes").getByText("part of this stake has left since the vote was cast").filter({ visible: true }).first()).toBeVisible();
    await page.getByRole("button", { name: "No", exact: true }).click();
    await noHorizontalScroll(page);
  });

  test("an executed proposal links its settle transaction and says the deposit was refunded", async ({ page }) => {
    await page.goto("/daos/2/proposals/6");
    await expect(page.getByTestId("dao-proposal-status")).toHaveText("Passed (executed)");
    await expect(page.getByTestId("dao-outcome").getByTestId("settle-tx")).toHaveAttribute("href", /^\/txs\/5555/);
    await expect(page.getByTestId("dao-deposit")).toContainText("refunded to the proposer");
  });

  test("a proposal whose execution failed shows the error and the retry deadline", async ({ page }) => {
    await page.goto("/daos/2/proposals/5");
    await expect(page.getByTestId("dao-proposal-status")).toHaveText("Failed to execute");
    await expect(page.getByTestId("execution-error")).toContainText("insufficient funds");
    await expect(page.getByTestId("retry-deadline")).toContainText("Nov 12, 2099");
  });

  test("a voting proposal past its end says anyone can settle it", async ({ page }) => {
    const res = await page.request.get("http://127.0.0.1:8089/api/v1/dao-proposals/7");
    await mockApi(page, { "v1/dao-proposals/7": { ...(await res.json()), voting_ended: true } });
    await page.goto("/daos/2/proposals/7");
    await expect(page.getByTestId("settle-note")).toHaveText("Voting ended; anyone can settle it now");
  });

  test("/dao-proposals/<id> redirects to the proposal under its DAO", async ({ page }) => {
    await page.goto("/dao-proposals/7");
    await expect(page).toHaveURL(/\/daos\/2\/proposals\/7$/);
    await expect(page.getByRole("heading", { level: 1, name: "Fund the hive festival" })).toBeVisible();
  });

  test("an unknown proposal is a not-found page", async ({ page }) => {
    await mockApi(page, { "v1/dao-proposals/99": respond(404, { error: { code: "not_found", message: "no" } }) });
    await page.goto("/daos/2/proposals/99");
    await expect(page.getByTestId("dao-proposal-not-found")).toBeVisible();
  });
});
