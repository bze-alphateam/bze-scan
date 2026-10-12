import { expect, test, type Page } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

import latest from "../test/fixtures/api/v1/blocks.json";
import block from "../test/fixtures/api/v1/blocks/25120440.json";
import status from "../test/fixtures/api/v1/status.json";

// The mock API answers /blocks from test/fixtures/api/v1/blocks.json, block
// 25120440 with its transfers and raw block, and 404s any other height. The
// status' latest block is 25120450; the e2e build shortens the pending window
// to 4 s.

const OLDER = { items: [{ ...latest.items[9], height: 25120439, hash: "B".repeat(64) }], next_cursor: null };

/** Answers the blocks list's older page (the mock API ignores the cursor). */
async function olderPage(page: Page) {
  await page.route(/\/api\/v1\/blocks\?/, async (route) => {
    const cursor = new URL(route.request().url()).searchParams.get("cursor");
    if (!cursor) return route.fallback();
    return route.fulfill({ json: OLDER, headers: { "Access-Control-Allow-Origin": "*", "Cache-Control": "no-store" } });
  });
}

test.describe("blocks list", () => {
  test("lists the latest blocks and loads an older page", async ({ page }) => {
    await olderPage(page);
    await page.goto("/blocks");
    await expect(page).toHaveTitle("Blocks · BZE Explorer");
    const rows = page.getByTestId("block-row");
    await expect(rows).toHaveCount(10);
    await expect(rows.first().getByRole("link", { name: "25,120,449" })).toHaveAttribute("href", "/blocks/25120449");
    await expect(rows.last().getByTestId("block-txs")).toContainText("7 · 1 failed");

    await page.getByRole("button", { name: "Show older blocks" }).click();
    await expect(rows).toHaveCount(11);
    await expect(rows.last().getByRole("link", { name: "25,120,439" })).toBeVisible();
    await expect(page.getByTestId("blocks-snapshot")).toBeVisible();

    await page.getByRole("button", { name: "Back to the latest blocks" }).click();
    await expect(rows).toHaveCount(10);
    await expect(page.getByTestId("blocks-snapshot")).toHaveCount(0);
  });
});

test.describe("block page", () => {
  test("renders the header, overview, transactions, grouped funds and raw block", async ({ page }) => {
    await page.goto(`/blocks/${block.height}`);
    await expect(page).toHaveTitle("Block 25,120,440 · BZE Explorer");
    await expect(page.getByRole("heading", { level: 1, name: "Block 25,120,440" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Next block" })).toHaveAttribute("href", "/blocks/25120441");
    await expect(page.getByTestId("block-chips")).toContainText("7 transactions");
    await expect(page.getByTestId("block-chips")).toContainText("1 failed");

    const overview = page.getByTestId("block-overview");
    await expect(overview).toContainText("0.022 BZE · collected from 3 transactions");
    await expect(overview.getByTestId("block-hash")).toHaveText(block.hash);

    const txs = page.getByTestId("block-tx");
    await expect(txs).toHaveCount(3);
    await expect(txs.first().getByTestId("block-tx-what")).toHaveText("Send 10 BZE to bze1m3k…9ds4");
    await expect(txs.nth(1).getByTestId("block-tx-what")).toContainText("+ 1 more action");

    const groups = page.getByTestId("flow-group");
    await expect(groups).toHaveCount(9);
    const dex = page.locator("[data-testid=flow-group][data-group=dex]");
    await expect(dex.getByTestId("flow-group-summary")).toHaveText("DEX · 600 BZE ↔ 7.5 USDC matched on the DEX");
    await dex.getByRole("button", { name: /Show/ }).click();
    await expect(dex.getByTestId("flow-step")).toHaveCount(2);
    await expect(dex.getByTestId("by-the-chain")).toHaveText("block 25,120,440, by the chain");

    await page.getByRole("button", { name: "Show all 18 transfers, one by one" }).click();
    await expect(page.getByTestId("block-flow-all").getByTestId("flow-step")).toHaveCount(18);

    await page.getByRole("button", { name: /More details/ }).click();
    await expect(page.getByText("474,968 used in this block")).toBeVisible();
    await expect(page.getByText(/"chain_id": "beezee-1"/)).toBeVisible();
    await expect(page.getByRole("link", { name: /Open on Ping.pub/ })).toHaveAttribute(
      "href",
      `https://explorer.getbze.com/beezee/block/${block.height}`,
    );
    await noHorizontalScroll(page);
  });

  test("waits for a height not produced yet", async ({ page }) => {
    const ahead = status.live_fill.db_height + 1;
    await page.goto(`/blocks/${ahead}`);
    await expect(page.getByRole("heading", { level: 1, name: "Block 25,120,451" })).toBeVisible();
    await expect(page.getByTestId("block-not_produced")).toContainText("Not produced yet");
    await expect(page.getByTestId("block-not_produced")).toContainText("Reload the page to look again", { timeout: 15_000 });
  });

  test("explains a height older than the explorer's history", async ({ page }) => {
    await page.goto("/blocks/24998000");
    await expect(page.getByTestId("block-too_old")).toContainText("(from block 24,998,316)");
  });

  test("answers not found for something that is not a height", async ({ page }) => {
    await page.goto("/blocks/0x10");
    await expect(page.getByRole("heading", { name: "Nothing here" })).toBeVisible();
  });
});

test.describe("blocks on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("stacks the list's rows and the block's tables within the screen", async ({ page }) => {
    await page.goto("/blocks");
    await expect(page.getByTestId("block-row")).toHaveCount(10);
    await noHorizontalScroll(page);

    await page.goto(`/blocks/${block.height}`);
    await expect(page.getByTestId("block-tx")).toHaveCount(3);
    await page.locator("[data-testid=flow-group][data-group=fees]").getByRole("button", { name: /Show/ }).click();
    await expect(page.getByTestId("flow-step")).toHaveCount(4);
    await noHorizontalScroll(page);
  });
});
