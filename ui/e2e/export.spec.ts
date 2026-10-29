import { readFile } from "node:fs/promises";

import { expect, test, type Page } from "@playwright/test";

import account from "../test/fixtures/api/v1/accounts/bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4.json";
import honey from "../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney.json";

// The CSV export (BFE-279). The mock API answers the first activity read
// (`?page=1`) with the account's 8 fixture rows and no next cursor, and the
// holders route with the 6 fixture holders whatever the cursor, so the
// holders export sets a minimum that ends the read on the first page.

/** Clicks Download CSV and returns the file's name and text (the byte-order mark dropped). */
async function download(page: Page): Promise<{ name: string; lines: string[] }> {
  const pending = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download CSV" }).click();
  const file = await pending;
  const text = await readFile((await file.path())!, "utf8");
  expect(text.startsWith("﻿")).toBe(true);
  return { name: file.suggestedFilename(), lines: text.slice(1).split("\r\n").filter(Boolean) };
}

test("exports the account's transactions as CSV, a line per denom moved", async ({ page }) => {
  await page.goto(`/accounts/${account.address}`);
  await expect(page.getByTestId("activity-row")).toHaveCount(8);
  await page.getByRole("button", { name: "Export" }).click();

  const modal = page.getByTestId("export-modal");
  await expect(modal.getByRole("heading", { name: "Export transactions" })).toBeVisible();
  await expect(modal.getByTestId("export-about")).toContainText("Each line is one token a transaction moved");
  await expect(modal.getByRole("combobox", { name: "Category" })).toHaveValue("");
  await expect(modal.getByRole("textbox", { name: "How many transactions at most" })).toHaveValue("10000");

  await modal.getByRole("button", { name: "Export" }).click();
  await expect(modal.getByTestId("export-status")).toHaveText("Ready: 8 transactions, 8 lines.");
  const { name, lines } = await download(page);
  expect(name).toMatch(new RegExp(`^transactions-${account.address}-\\d{8}-\\d{6}\\.csv$`));
  expect(lines[0]).toBe(
    "Time (UTC),Block,Transaction hash,Description,Category,Kind,Amount,Symbol,Fee,Fee symbol,Counterparty,Counterparty name,Success",
  );
  expect(lines).toHaveLength(9);
  expect(lines[1]).toMatch(/^2026-10-12 09:58:00,25120440,1+,/);
  expect(lines[1]).toContain(",sent,send,-10,BZE,0.002,BZE,");
  // The DEX fill the queue settled at the end of the block: no transaction hash.
  expect(lines[2]).toMatch(/^2026-10-12 09:41:00,25120301,,/);
});

test("exports the transactions of one category, leaving out rows without a transaction", async ({ page }) => {
  await page.goto(`/accounts/${account.address}?tab=activity&category=dex`);
  await page.getByRole("button", { name: "Export" }).click();
  const modal = page.getByTestId("export-modal");
  await expect(modal.getByRole("combobox", { name: "Category" })).toHaveValue("dex");
  await modal.getByRole("combobox", { name: "Category" }).selectOption({ label: "Sent" });
  await modal.getByText("Include rows without a transaction").click();
  await modal.getByRole("button", { name: "Export" }).click();
  await expect(modal.getByTestId("export-status")).toHaveText("Ready: 1 transactions, 1 line.");
  const { lines } = await download(page);
  expect(lines).toHaveLength(2);
  expect(lines[1]).toContain(",sent,send,");
});

test("exports a token's holders over a minimum, module accounts left out", async ({ page }) => {
  await page.goto(`/tokens/${encodeURIComponent(honey.denom)}`);
  await expect(page.getByTestId("holder-row")).toHaveCount(6);
  await page.getByRole("button", { name: "Export" }).click();

  const modal = page.getByTestId("export-modal");
  await expect(modal.getByRole("heading", { name: "Export HONEY holders" })).toBeVisible();
  await expect(modal.getByTestId("export-about")).toContainText("largest balance first");
  await expect(modal.getByRole("checkbox", { name: /Leave out module accounts/ })).toBeChecked();
  await expect(modal.getByRole("textbox", { name: "How many holders at most" })).toHaveValue("20000");

  await modal.getByRole("textbox", { name: "Minimum balance (HONEY)" }).fill("50000");
  await modal.getByRole("button", { name: "Export" }).click();
  await expect(modal.getByTestId("export-status")).toHaveText("Ready: 2 holders, 2 lines.");
  const { name, lines } = await download(page);
  expect(name).toMatch(/^holders-HONEY-\d{8}-\d{6}\.csv$/);
  expect(lines).toEqual([
    "Rank,Address,Name,Balance,Symbol,Share of supply (%)",
    "2,bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk,,100000,HONEY,20.00000",
    "3,bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4,,50000,HONEY,10.00000",
  ]);
});
