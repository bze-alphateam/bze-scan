import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

// Chain v8.3.0 messages in plain words: the mock API answers these hashes from
// test/fixtures/api/v1/txs. Runs on the desktop and the phone project.
const HALT = "E1A2B3C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B4C5D6E7F80";
const DAO_PROPOSAL = "D4A0C3B2A1F0E9D8C7B6A5F4E3D2C1B0A9F8E7D6C5B4A3F2E1D0C9B8A7F6E5D4";

test.describe("chain v8.3.0 transactions", () => {
  test("shows a guardian's trading halt as an emergency action", async ({ page }) => {
    await page.goto(`/txs/${HALT}`);
    const card = page.getByTestId("action-card");
    await expect(card).toHaveCount(1);
    await expect(card.getByRole("heading", { name: /^Emergency: halted trading of / })).toBeVisible();
    await expect(card.getByTestId("emergency-badge")).toHaveText("Emergency");
    await expect(card).toContainText("Tokens halted");
    await noHorizontalScroll(page);
  });

  test("shows a DAO proposal with what it would do", async ({ page }) => {
    await page.goto(`/txs/${DAO_PROPOSAL}`);
    const card = page.getByTestId("action-card");
    await expect(card.getByRole("heading", { name: "Proposed “Fund the market maker” in DAO #3" })).toBeVisible();
    await expect(card.getByText("What it would do")).toBeVisible();
    await expect(card.getByTestId("nested-message")).toContainText("Send 5 BZE to bze1m3k…9ds4");
    await noHorizontalScroll(page);
  });
});
