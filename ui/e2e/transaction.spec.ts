import { expect, test } from "@playwright/test";

import failedTx from "../test/fixtures/api/v1/txs/7D04E1A2B3C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B40E9B0.json";
import sendTx from "../test/fixtures/api/v1/txs/3F9A6B2C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B4C5D0C21E.json";

// The mock API answers these hashes from test/fixtures/api/v1/txs and 404s any
// other; the e2e build shortens the pending window to 4 s.
const SEND = sendTx.hash;
const FAILED = failedTx.hash;
const UNKNOWN = "0".repeat(60) + "BEEF";

test.describe("transaction page", () => {
  test("renders a send with its funds, overview and raw JSON", async ({ page }) => {
    // Any case in the URL; the page asks for the upper-case hash.
    await page.goto(`/txs/${SEND.toLowerCase()}`);
    await expect(page).toHaveTitle(/^Transaction 3F9A…C21E · Success · BZE Explorer$/);

    await expect(page.getByRole("heading", { level: 1, name: "Transaction" })).toBeVisible();
    await expect(page.getByText("Success", { exact: true })).toBeVisible();
    await expect(page.getByTestId("tx-hash")).toHaveText(SEND);
    await expect(page.getByRole("link", { name: "25,120,440" }).first()).toHaveAttribute("href", "/blocks/25120440");

    const actions = page.getByTestId("action-card");
    await expect(actions).toHaveCount(1);
    await expect(actions.getByRole("heading", { name: "Send 10 BZE to bze1m3k…9ds4" })).toBeVisible();

    await expect(page.getByTestId("flow-summary")).toHaveText(
      "2 transfers · 0.002 BZE fee to Fee collector · 10 BZE to bze1m3k…9ds4",
    );
    await page.getByRole("button", { name: /Show steps/ }).click();
    await expect(page.getByTestId("flow-step")).toHaveCount(2);
    await expect(page.getByTestId("flow-step").first()).toContainText("transaction fee");

    const overview = page.locator("section", { has: page.getByRole("heading", { name: "Overview" }) });
    await expect(overview).toContainText("10 confirmations");
    await expect(overview).toContainText("3rd of 7 transactions in the block");
    await expect(overview).toContainText("rent for October");

    await page.getByRole("button", { name: /More details/ }).click();
    await expect(page.getByText("81,234 of 200,000")).toBeVisible();
    await expect(page.getByText(/"gas_used": "81234"/)).toBeVisible();
    await expect(page.getByRole("link", { name: /Open on Ping.pub/ })).toHaveAttribute("href", `https://explorer.getbze.com/beezee/tx/${SEND}`);

    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });

  test("explains a failed transaction", async ({ page }) => {
    await page.goto(`/txs/${FAILED}`);
    await expect(page).toHaveTitle(/· Failed · BZE Explorer$/);
    await expect(page.getByText("Failed", { exact: true })).toBeVisible();
    await expect(page.getByRole("alert")).toContainText("Not enough BZE for the amount plus the fee");
    await expect(page.getByTestId("action-card")).toContainText("not executed");
    await expect(page.getByTestId("flow-summary")).toHaveText("1 transfer · 0.002 BZE fee to Fee collector");
  });

  test("looks for a pending transaction, then says it is not found", async ({ page }) => {
    await page.goto(`/txs/${UNKNOWN}`);
    await expect(page).toHaveTitle(/· Pending · BZE Explorer$/);
    await expect(page.getByTestId("tx-looking")).toContainText("Looking for this transaction…");
    await expect(page.getByTestId("tx-not_found")).toContainText("Not found.", { timeout: 15_000 });
    await expect(page.getByTestId("tx-not_found").getByRole("link", { name: /Ping.pub/ })).toHaveAttribute(
      "href",
      `https://explorer.getbze.com/beezee/tx/${UNKNOWN}`,
    );
    await expect(page.getByText("Failed", { exact: true })).toHaveCount(0);
  });

  test("answers not found for something that is not a hash", async ({ page }) => {
    await page.goto("/txs/not-a-hash");
    await expect(page.getByRole("heading", { name: "Nothing here" })).toBeVisible();
  });
});

test.describe("transaction page on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("stacks each step and keeps the page within the screen", async ({ page }) => {
    await page.goto(`/txs/${SEND}`);
    await page.getByRole("button", { name: /Show steps/ }).click();
    const step = page.getByTestId("flow-step").nth(1);
    const [from, to] = await Promise.all([
      step.getByRole("link").first().boundingBox(),
      step.getByRole("link").last().boundingBox(),
    ]);
    expect(from && to && to.y > from.y).toBe(true);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });
});
