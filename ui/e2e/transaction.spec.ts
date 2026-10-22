import { expect, test, type Page } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

import dexTx from "../test/fixtures/api/v1/txs/5D1E0A7B3C9F42E6A8D0B1C2D3E4F5061728394A5B6C7D8E9F0A1B2C3D4E5F60.json";
import ibcTx from "../test/fixtures/api/v1/txs/9C2B4E6A8D0F1325476980ABCDEF0123456789ABCDEF0123456789ABCDEF0A7E.json";
import failedTx from "../test/fixtures/api/v1/txs/7D04E1A2B3C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B40E9B0.json";
import raffleTx from "../test/fixtures/api/v1/txs/B0A1F2E3D4C5B6A79881726354453627180918273645546372819203A4B5C6D7.json";
import voteTx from "../test/fixtures/api/v1/txs/C0DE47A1B2C3D4E5F60718293A4B5C6D7E8F90A1B2C3D4E5F60718293A4B5C6D.json";
import sendTx from "../test/fixtures/api/v1/txs/3F9A6B2C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B4C5D0C21E.json";
import refundedTx from "../test/fixtures/api/v1/txs/F111ED0A7B3C9F42E6A8D0B1C2D3E4F5061728394A5B6C7D8E9F0A1B2C3D4E5F.json";

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
    await expect(page.locator("dt", { hasText: "Gas" }).locator("xpath=following-sibling::dd[1]")).toHaveText(/^81,234\s*of\s*200,000$/);
    await expect(page.getByText(/"gas_used": "81234"/)).toBeVisible();
    await expect(page.getByRole("link", { name: /Open on Ping.pub/ })).toHaveAttribute("href", `https://explorer.getbze.com/beezee/tx/${SEND}`);

    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });

  test("explains a failed transaction", async ({ page }) => {
    await page.goto(`/txs/${FAILED}`);
    await expect(page).toHaveTitle(/· Failed · BZE Explorer$/);
    await expect(page.getByText("Failed", { exact: true })).toBeVisible();
    // Next.js adds its own role="alert" (the route announcer) to every page.
    await expect(page.getByRole("alert").filter({ hasText: "Why it failed" })).toContainText("Not enough BZE for the amount plus the fee");
    await expect(page.getByTestId("action-card")).toContainText("not executed");
    await expect(page.getByTestId("flow-summary")).toHaveText("1 transfer · 0.002 BZE fee to Fee collector");
  });

  test("looks for a pending transaction, then says it is not found", async ({ page }) => {
    await page.goto(`/txs/${UNKNOWN}`);
    await expect(page).toHaveTitle(/· Pending · BZE Explorer$/);
    await expect(page.getByTestId("tx-looking")).toContainText("Looking for this transaction…");
    await expect(page.getByTestId("tx-not_found")).toContainText("Not found", { timeout: 15_000 });
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

/**
 * Answers the transaction route with `first` until `switchTo()` is called,
 * then with `then` (the mock API serves one fixture per hash).
 */
async function changingTx(page: Page, hash: string, first: unknown, then: unknown) {
  let switched = false;
  await page.route(`**/api/v1/txs/${hash}`, (route) =>
    route.fulfill({ json: switched ? then : first, headers: { "Access-Control-Allow-Origin": "*", "Cache-Control": "no-store" } }),
  );
  return () => {
    switched = true;
  };
}

test.describe("transaction page with BZE actions", () => {
  test("renders a DEX order and a claim with the outcome, the reasons and the net", async ({ page }) => {
    await page.goto(`/txs/${dexTx.hash}`);
    const actions = page.getByTestId("action-card");
    await expect(actions).toHaveCount(2);
    await expect(actions.first().getByRole("heading", { name: "Buy order · 1,000 BZE at 0.0125 USDC" })).toBeVisible();
    await expect(actions.first().getByTestId("dex-fills")).toContainText("Filled right away: 600 BZE for 7.5 USDC");
    await expect(actions.first().getByTestId("dex-placed")).toContainText("400 BZE still waiting on the order book");
    await expect(actions.first().getByTestId("dex-filled")).toHaveText("60% filled");
    await expect(actions.nth(1).getByRole("heading", { name: "Claim staking-reward prizes · 1.5 BZE" })).toBeVisible();

    const steps = page.getByTestId("flow-step");
    await expect(steps).toHaveCount(5);
    await expect(steps.nth(1)).toContainText("DEX fee");
    await expect(steps.nth(3)).toContainText("held for the buy order");
    await expect(steps.nth(4)).toContainText("staking-reward prize");
    await expect(page.getByTestId("net-box")).toBeVisible();
  });

  test("says what a fill message that found no orders got back (chain v8.3.0)", async ({ page }) => {
    await page.goto(`/txs/${refundedTx.hash}`);
    const card = page.getByTestId("action-card");
    await expect(card.getByRole("heading", { name: "Fill 2 orders on BZE/USDC" })).toBeVisible();
    await expect(card.getByTestId("dex-not-filled")).toHaveText(
      "Nothing was filled (no order at that price): 16,087.378657 BZE sent back.",
    );
    await expect(card.getByTestId("dex-fills")).toHaveCount(0);
    await expect(card.getByTestId("dex-pending")).toHaveCount(0);
  });

  test("shows a pending order, then its outcome once the order book processed it", async ({ page }) => {
    const pending = { ...dexTx, dex: { placed: [], fills: [], cancelled: [], pending: true } };
    const resolve = await changingTx(page, dexTx.hash, pending, dexTx);
    await page.goto(`/txs/${dexTx.hash}`);
    await expect(page.getByTestId("dex-pending")).toContainText("The order book processes this at the end of the block");
    resolve();
    await expect(page.getByTestId("dex-fills")).toBeVisible({ timeout: 15_000 });
    await expect(page.getByTestId("dex-pending")).toHaveCount(0);
  });

  test("shows a cross-chain transfer pending, then delivered", async ({ page }) => {
    const row = ibcTx.ibc_transfers[0];
    const delivered = {
      ...ibcTx,
      ibc_transfers: [{ ...row, status: "delivered", result_height: 25120452, result_tx_hash: "CD".repeat(32), result_time: "2026-10-11T09:16:00Z" }],
    };
    const deliver = await changingTx(page, ibcTx.hash, ibcTx, delivered);
    await page.goto(`/txs/${ibcTx.hash}`);
    await expect(page.getByRole("heading", { name: "Cross-chain transfer to Cosmos Hub · 2 ATOM" })).toBeVisible();
    const status = page.getByTestId("ibc-status");
    await expect(status).toContainText("Pending");
    await expect(status).toContainText("waiting for Cosmos Hub to confirm");
    await expect(status.getByRole("link", { name: /See on Cosmos Hub's explorer/ })).toHaveAttribute(
      "href",
      `https://www.mintscan.io/cosmos/address/${row.receiver}`,
    );
    deliver();
    await expect(status).toContainText("Delivered", { timeout: 15_000 });
    await expect(status.getByRole("link", { name: "CDCD…CDCD" })).toHaveAttribute("href", `/txs/${"CD".repeat(32)}`);
  });
});

test.describe("BZE actions on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("keeps the DEX outcome, the steps and the net within the screen", async ({ page }) => {
    await page.goto(`/txs/${dexTx.hash}`);
    await expect(page.getByTestId("flow-step")).toHaveCount(5);
    await expect(page.getByTestId("dex-fills")).toBeVisible();
    await noHorizontalScroll(page);
    await page.goto(`/txs/${ibcTx.hash}`);
    await expect(page.getByTestId("ibc-status")).toBeVisible();
    await noHorizontalScroll(page);
  });
});

test.describe("links to the BZE apps", () => {
  test("a raffle opens its coin on the Burner, a vote its proposal on Staking", async ({ page }) => {
    await page.goto(`/txs/${raffleTx.hash}`);
    const raffle = page.getByTestId("tx-apps");
    await expect(raffle).toHaveText(/Open raffle on\s*Burner/);
    await expect(raffle.getByRole("link", { name: /Burner/ })).toHaveAttribute(
      "href",
      `https://burner.getbze.com/coin?coin=${encodeURIComponent(raffleTx.messages[0].body.denom)}`,
    );

    await page.goto(`/txs/${voteTx.hash}`);
    const vote = page.getByTestId("tx-apps");
    await expect(vote).toHaveText(/Vote on\s*Staking/);
    await expect(vote.getByRole("link", { name: /Staking/ })).toHaveAttribute("href", "https://staking.getbze.com/governance/47");

    await page.goto(`/txs/${SEND}`);
    await expect(page.getByRole("heading", { level: 1, name: "Transaction" })).toBeVisible();
    await expect(page.getByTestId("tx-apps")).toHaveCount(0);
  });
});
