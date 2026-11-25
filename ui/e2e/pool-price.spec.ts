import { expect, test } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

import lp from "../test/fixtures/api/v1/tokens/ulp_factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney_ubze.json";

// An LP token page with the pool price card: the mock LP token's spot 2 is 11.1% away from its average 1.8.
test("an LP token shows the pool price, its drift and what it means", async ({ page }) => {
  await page.goto(`/tokens/${encodeURIComponent(lp.denom)}`);
  await expect(page.getByTestId("pool-price")).toBeVisible();
  await expect(page.getByTestId("pool-price-drift")).toHaveText("11.1% away");
  await expect(page.getByTestId("pool-status-fees")).toContainText("charged in BZE for now");
  await expect(page.getByTestId("pool-status-module")).toContainText("on hold");
  await page.getByTestId("pool-price-help").click();
  await expect(page.getByRole("dialog")).toContainText("charged in BZE instead");
  await page.keyboard.press("Escape");
  await noHorizontalScroll(page);
});
