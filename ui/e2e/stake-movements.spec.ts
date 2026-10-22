import { expect, test, type Page } from "@playwright/test";

import { noHorizontalScroll } from "./layout";
import { mockApi } from "./mock-api";

import changes from "../test/fixtures/api/v1/validators/stake-changes.json";
import honeycomb from "../test/fixtures/api/v1/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c.json";
import movements from "../test/fixtures/api/v1/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c/movements.json";

// The mock API answers /validators/stake-changes from validators/stake-changes.json
// (Honeycomb grew, Thamar Node shrank, Deep Fields moved over 7 days only,
// Sleepy Node netted 0, Old Timer is absent) and Honeycomb's movements from
// validators/<operator>/movements.json (12 rows, 10 a page).
const OP = honeycomb.operator_address;

const rows = (page: Page) => page.getByTestId("validator-row");

async function openActive(page: Page) {
  await page.goto("/validators");
  await page.getByRole("tab", { name: "Active 3" }).click();
  await expect(rows(page)).toHaveCount(3);
}

test.describe("stake change on the validators list", () => {
  test.skip(({ isMobile }) => !!isMobile, "desktop layout");

  test("fills the Change column: growth up, shrink down, nothing for a zero window", async ({ page }) => {
    await openActive(page);
    await expect(page.getByText("Change", { exact: true })).toBeVisible();
    const cell = (i: number) => rows(page).nth(i).getByTestId("validator-change");
    await expect(cell(0)).toHaveText("24h ▲ +122.0K7d ▲ +185.0K");
    await expect(cell(1)).toHaveText("24h ▼ −1.64M7d ▼ −2.93M");
    await expect(cell(2)).toHaveText("7d ▲ +48.5K");
    await expect(cell(1).locator("[data-dir=down]")).toHaveCount(2);
    await noHorizontalScroll(page);

    await page.getByRole("tab", { name: "Inactive 2" }).click();
    await expect(rows(page)).toHaveCount(2);
    await expect(rows(page).getByTestId("stake-change")).toHaveCount(0);
  });

  test("shows no change while the window is still being indexed", async ({ page }) => {
    await mockApi(page, { "v1/validators/stake-changes": { ...changes, complete: false } });
    await openActive(page);
    await expect(rows(page).first()).toContainText("50M BZE");
    await expect(rows(page).getByTestId("stake-change")).toHaveCount(0);
  });
});

test.describe("stake change on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("puts the change on a line under the stake", async ({ page }) => {
    await openActive(page);
    const row = rows(page).nth(1);
    const line = row.getByTestId("stake-change").filter({ visible: true });
    await expect(line).toHaveText("24h ▼ −1.64M7d ▼ −2.93M");
    const [staked, change] = await Promise.all([row.getByText("42.1M BZE").filter({ visible: true }).boundingBox(), line.boundingBox()]);
    expect(staked && change && change.y > staked.y).toBe(true);
    await noHorizontalScroll(page);

    await page.goto(`/validators/${OP}`);
    await expect(page.getByTestId("stake-movement")).toHaveCount(10);
    await noHorizontalScroll(page);
    // Pager squares are big enough to tap.
    const square = await page.getByRole("link", { name: "Page 2" }).boundingBox();
    expect(square && square.height >= 44 && square.width >= 44).toBe(true);
  });
});

test.describe("validator page with stake movements", () => {
  test("shows the change under the stake and the sections in order", async ({ page }) => {
    await page.goto(`/validators/${OP}`);
    const staked = page.getByTestId("tile").first();
    await expect(staked).toContainText("50M BZE");
    await expect(staked.getByTestId("stake-change")).toHaveText("24h ▲ +122.0K7d ▲ +185.0K");
    await expect(staked).toContainText("12.3% of all staked BZE");

    await expect(page.getByRole("heading", { level: 2 })).toHaveText([
      "About",
      "Recent blocks proposed",
      "Recent stake movements",
      "History",
      "Last governance votes",
    ]);
    const history = page.locator("section", { has: page.getByRole("heading", { name: "History" }) });
    await expect(history.getByTestId("validator-vote")).toHaveCount(0);
    const votes = page.locator("section", { has: page.getByRole("heading", { name: "Last governance votes" }) });
    await expect(votes.getByTestId("validator-vote")).toHaveCount(2);
    await expect(page.getByRole("button", { name: /More details/ })).toBeVisible();
  });

  test("pages the movements and links each row to its transaction", async ({ page }) => {
    await page.goto(`/validators/${OP}`);
    const section = page.getByTestId("validator-movements");
    const items = section.getByTestId("stake-movement");
    await expect(items).toHaveCount(10);
    await expect(section.getByTestId("stake-movements-range")).toHaveText("Movements 1–10 of 12");
    await expect(items.nth(1).getByTestId("stake-movement-what")).toHaveText("Moved to Thamar Node");
    await expect(items.nth(1).getByTestId("stake-movement-amount")).toHaveText("−40,000 BZE");
    await expect(items.first().getByTestId("stake-movement-tx")).toHaveAttribute("href", `/txs/${movements.items[0].tx_hash}`);

    await section.getByRole("link", { name: "Page 2" }).click();
    await expect(items).toHaveCount(2);
    await expect(section.getByTestId("stake-movements-range")).toHaveText("Movements 11–12 of 12");
    await expect(page).toHaveURL(`/validators/${OP}?movements=2`);

    await items.first().getByTestId("stake-movement-tx").click();
    await expect(page).toHaveURL(`/txs/${movements.items[10].tx_hash}`);
  });

  test("says when the window is still being indexed or holds nothing", async ({ page }) => {
    await mockApi(page, {
      "v1/validators/stake-changes": { ...changes, complete: false },
      [`v1/validators/${OP}/movements`]: { ...movements, complete: false, total: 0, items: [] },
    });
    await page.goto(`/validators/${OP}`);
    await expect(page.getByTestId("stake-movements-empty")).toHaveText("No stake movements in the last 7 days.");
    await expect(page.getByTestId("stake-movements-incomplete")).toHaveText("The last 7 days are still being indexed.");
    await expect(page.getByTestId("tile").first().getByTestId("stake-change")).toHaveCount(0);
  });
});
