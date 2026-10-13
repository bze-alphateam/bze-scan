import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

import block from "../test/fixtures/api/v1/blocks/25120440.json";
import status from "../test/fixtures/api/v1/status.json";

// The mock API answers /blocks from test/fixtures/api/v1/blocks.json (newest
// 25,120,449), /blocks?before=H with the 25 heights below H, block 25120440
// with its transfers and raw block, and 404s any other height. The status'
// latest block is 25120450; the e2e build shortens the pending window to 4 s.

test.describe("blocks list", () => {
  test("lists the latest blocks, pins page 2 to the newest height and goes back to the latest", async ({ page }) => {
    await page.goto("/blocks");
    await expect(page).toHaveTitle("Blocks · BZE Explorer");
    const rows = page.getByTestId("block-row");
    await expect(rows).toHaveCount(10);
    await expect(rows.first().getByRole("link", { name: "25,120,449" })).toHaveAttribute("href", "/blocks/25120449");
    await expect(rows.last().getByTestId("block-txs")).toContainText("7 · 1 failed");
    await expect(page.getByTestId("pager-current")).toHaveText("1");
    await expect(page.getByRole("link", { name: "Previous page" })).toHaveAttribute("aria-disabled", "true");

    await page.getByRole("link", { name: "Next page" }).click();
    await expect(page).toHaveURL(/\/blocks\?top=25120449&page=2$/);
    await expect(rows).toHaveCount(25);
    await expect(page.getByTestId("blocks-range")).toHaveText("Blocks 25,120,424 – 25,120,400");
    await expect(page.getByTestId("pager-current")).toHaveText("2");
    await expect(page.getByRole("link", { name: "Last page" })).toHaveAttribute("href", "/blocks?top=25120449&page=4886");

    const fresh = page.getByTestId("blocks-new");
    await expect(fresh).toHaveText("1 new block · back to latest");
    await fresh.click();
    await expect(page).toHaveURL(/\/blocks$/);
    await expect(rows).toHaveCount(10);
    await expect(page.getByTestId("blocks-new")).toHaveCount(0);
  });

  test("jumps to a typed page, clamped to the history", async ({ page }) => {
    await page.goto("/blocks?top=25120449&page=2");
    await page.getByRole("textbox", { name: "Page" }).fill("99999");
    await page.getByRole("button", { name: "Go to page" }).click();
    await expect(page).toHaveURL(/\/blocks\?top=25120449&page=4886$/);
    await expect(page.getByTestId("blocks-range")).toHaveText("Blocks 24,998,324 – 24,998,316");
    await expect(page.getByRole("link", { name: "Next page" })).toHaveAttribute("aria-disabled", "true");
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
    await expect(overview).toContainText("0.022 BZE· collected from 3 transactions");
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
    await expect(page.getByText("474,968", { exact: true })).toBeVisible();
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
    await expect(page.getByTestId("block-too_old").getByRole("link", { name: /Ping.pub/ })).toHaveAttribute(
      "href",
      "https://explorer.getbze.com/beezee/block/24998000",
    );
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
    // The page box wraps under the arrows, so it drops the divider it has beside them.
    await expect(page.getByTestId("pager-jump")).toHaveCSS("border-left-width", "0px");
    await expect(page.getByTestId("pager-jump")).toHaveCSS("margin-left", "0px");

    await page.goto(`/blocks/${block.height}`);
    await expect(page.getByTestId("block-tx")).toHaveCount(3);
    await page.locator("[data-testid=flow-group][data-group=fees]").getByRole("button", { name: /Show/ }).click();
    await expect(page.getByTestId("flow-step")).toHaveCount(4);
    await noHorizontalScroll(page);

    // The raw JSON tabs stay on one line each; Ping.pub moves under the JSON.
    await page.getByRole("button", { name: /More details/ }).click();
    const heights = await page.getByRole("tab").evaluateAll((tabs) => tabs.map((t) => t.getBoundingClientRect().height));
    expect(heights).toHaveLength(3);
    expect(new Set(heights).size).toBe(1);
    expect(heights[0]).toBeLessThan(48);
    const ping = page.getByRole("link", { name: /Open on Ping.pub/ });
    await expect(ping).toHaveCount(1);
    expect((await ping.boundingBox())?.height).toBeLessThan(24);
    await noHorizontalScroll(page);
  });
});
