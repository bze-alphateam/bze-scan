import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

import honeycomb from "../test/fixtures/api/v1/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c.json";

// The mock API answers /validators from test/fixtures/api/v1/validators.json
// (three active, two inactive), Honeycomb's page from validators/<operator>.json
// and /params from params.json.
const OP = honeycomb.operator_address;

const rows = (page: import("@playwright/test").Page) => page.getByTestId("validator-row");

test.describe("validators list", () => {
  test("renders the tabs, filters by name and links to a validator", async ({ page }) => {
    await page.goto("/validators");
    await expect(page).toHaveTitle("Validators · BZE Explorer");
    await expect(page.getByText(/3 validators produce blocks right now, out of 40 seats/)).toBeVisible();
    await expect(page.getByTestId("tile")).toHaveCount(4);
    await expect(page.getByTestId("tile").first()).toContainText("405M BZE");

    await expect(page.getByRole("tab", { name: "Active · 3" })).toHaveAttribute("aria-selected", "true");
    await expect(rows(page)).toHaveCount(3);
    await expect(rows(page).first()).toContainText("Honeycomb");

    await page.getByRole("tab", { name: "Inactive · 2" }).click();
    await expect(rows(page)).toHaveCount(2);
    await expect(rows(page).first()).toContainText("Sleepy Node");
    await expect(rows(page).first()).toContainText("Jailed");

    await page.getByRole("tab", { name: "Active · 3" }).click();
    await page.getByRole("textbox", { name: "Find a validator" }).fill("thamar");
    await expect(rows(page)).toHaveCount(1);
    await expect(rows(page).first()).toContainText("Thamar Node");

    await page.getByRole("textbox", { name: "Find a validator" }).fill("honey");
    // Exact: the row's "Stake with Honeycomb" button is a link too.
    await rows(page).first().getByRole("link", { name: "Honeycomb", exact: true }).click();
    await expect(page).toHaveURL(`/validators/${OP}`);
    await expect(page.getByRole("heading", { level: 1, name: "Honeycomb" })).toBeVisible();
    await noHorizontalScroll(page);
  });
});

test.describe("validator page", () => {
  test("renders the tiles, about, recent blocks, history and More details", async ({ page }) => {
    await page.goto(`/validators/${OP}`);
    await expect(page).toHaveTitle("Honeycomb · validator · BZE Explorer");
    await expect(page.getByTestId("validator-status")).toHaveText("Active · rank 1 of 3");

    const tiles = page.getByTestId("tile");
    await expect(tiles).toHaveCount(4);
    await expect(tiles.nth(2)).toContainText("≈ 8.5%");
    await expect(tiles.nth(3)).toContainText("missed 1 of the last 10,000 blocks");

    const about = page.locator("section", { has: page.getByRole("heading", { name: "About" }) });
    await expect(about).toContainText("self-stake and commission land here");
    await expect(page.getByTestId("recent-block")).toHaveCount(3);
    await expect(page.getByTestId("history-item").first()).toContainText("Commission changed from 4% to 5%");
    await expect(page.getByText("Never jailed, never penalised since Apr 17, 2022")).toBeVisible();
    await expect(page.getByRole("link", { name: "#14 Raise the block size" })).toHaveAttribute("href", "/proposals/14");

    await page.getByRole("button", { name: /More details/ }).click();
    // Exact: the raw JSON below holds the key too.
    await expect(page.getByText("RQzL8AijestH86OmhuuzMn/L7HNNARaDoONs2H+Wo+c=", { exact: true })).toBeVisible();
    await expect(page.getByText(/"delegator_count": 1204/)).toBeVisible();
    await expect(page.getByRole("link", { name: /Open on Ping.pub/ })).toHaveAttribute(
      "href",
      `https://explorer.getbze.com/beezee/staking/${OP}`,
    );
    await noHorizontalScroll(page);
  });

  test("answers not found for something that is not an operator address", async ({ page }) => {
    await page.goto("/validators/bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw");
    await expect(page.getByRole("heading", { name: "Nothing here" })).toBeVisible();
  });
});

test.describe("validators on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("stacks each validator into a card and keeps the pages within the screen", async ({ page }) => {
    await page.goto("/validators");
    const row = rows(page).first();
    const [name, staked] = await Promise.all([
      row.getByRole("link", { name: "Honeycomb", exact: true }).boundingBox(),
      row.getByText("50M BZE").boundingBox(),
    ]);
    expect(name && staked && staked.y > name.y).toBe(true);
    await noHorizontalScroll(page);

    await page.goto(`/validators/${OP}`);
    await expect(page.getByTestId("tile")).toHaveCount(4);
    await noHorizontalScroll(page);
  });
});
