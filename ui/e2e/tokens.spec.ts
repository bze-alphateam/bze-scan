import { expect, test, type Page } from "@playwright/test";

import { noHorizontalScroll } from "./layout";

import honey from "../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney.json";
import usdc from "../test/fixtures/api/v1/tokens/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4.json";

// The mock API answers /tokens from test/fixtures/api/v1/tokens.json (every
// kind; the query is ignored, so the chips filter on the page) and a token's
// routes from tokens/<denom>.json and tokens/<denom>/{holders,transfers}.json,
// the URL-encoded denom decoded into folders.
const rows = (page: Page) => page.getByTestId("token-list-row");
const tokenPath = (denom: string) => `/tokens/${encodeURIComponent(denom)}`;

test.describe("tokens list", () => {
  test("renders the tiles, filters by chip and name, and links to a token", async ({ page }) => {
    await page.goto("/tokens");
    await expect(page).toHaveTitle("Tokens · BZE Explorer");
    await expect(page.getByText("Every token on BZE. Prices come from the BZE DEX.")).toBeVisible();
    await expect(page.getByTestId("tile")).toHaveCount(4);
    await expect(page.getByTestId("tile").first()).toContainText("1 native · 2 Factory · 2 cross-chain");
    await expect(rows(page)).toHaveCount(6);
    await expect(rows(page).nth(3)).toContainText("Halted on the DEX");
    // The pool share shows its two assets' logos; a one-hop token has no hops tag.
    await expect(rows(page).nth(5).getByTestId("lp-logo")).toBeVisible();
    await expect(page.getByTestId("hops-tag")).toHaveCount(0);

    await page.getByRole("button", { name: "Cross-chain", exact: true }).click();
    await expect(rows(page)).toHaveCount(2);
    await expect(rows(page).first().getByTestId("token-badges")).toContainText("Cross-chainCosmos Hub");
    // A badge explains itself on tap.
    await rows(page).first().getByTestId("direct-tag").click();
    await expect(page.getByRole("dialog")).toContainText("ATOM comes from Cosmos Hub, sent straight to BZE.");
    await page.keyboard.press("Escape");

    await page.getByRole("button", { name: "All", exact: true }).click();
    await page.getByRole("textbox", { name: "Find a token" }).fill("honey");
    await expect(rows(page)).toHaveCount(1);
    await rows(page).first().getByTestId("token-link").click();
    await expect(page).toHaveURL(/\/tokens\/factory(%2F|\/)bze13gzq/);
    await expect(page.getByRole("heading", { level: 1, name: "HONEY" })).toBeVisible();
    await noHorizontalScroll(page);
  });
});

test.describe("token page", () => {
  test("a Factory token shows the orange supply box, its top holders and activity", async ({ page }) => {
    await page.goto(tokenPath(honey.denom));
    await expect(page).toHaveTitle("HONEY · Honey · BZE Explorer");
    await expect(page.getByTestId("token-chips")).toContainText("Factory");
    await expect(page.getByTestId("supply-box")).toHaveAttribute("data-state", "admin");
    await expect(page.getByTestId("supply-box")).toContainText("So far: 2 mints adding 1,000,500 HONEY, 1 burn removing 200 HONEY.");
    await expect(page.getByTestId("holder-row")).toHaveCount(6);
    await expect(page.getByText("the top 5 hold 87.5%")).toBeVisible();
    await expect(page.getByTestId("activity-item")).toHaveCount(7);
    await expect(page.getByTestId("activity-item").first()).toContainText("25,000 HONEY");

    await page.getByRole("button", { name: /More details/ }).click();
    await expect(page.getByTestId("token-denom")).toHaveText(honey.denom);
    await noHorizontalScroll(page);
  });

  test("the plain-path form of a denom opens the same page", async ({ page }) => {
    await page.goto(`/tokens/${honey.denom}`);
    await expect(page.getByRole("heading", { level: 1, name: "HONEY" })).toBeVisible();
  });

  test("a cross-chain token shows its origin chain", async ({ page }) => {
    await page.goto(tokenPath(usdc.denom));
    await expect(page.getByTestId("token-chips")).toContainText("Cross-chainNoble");
    await expect(page.getByTestId("ibc-connection")).toContainText("open and active");
    await expect(page.getByTestId("supply-box")).toContainText("USDC is minted on Noble");
    await expect(page.getByTestId("token-about")).toContainText("Base denom thereuusdc");
    await expect(page.getByText("No activity indexed yet.")).toBeVisible();
  });

  test("an unknown token answers not found", async ({ page }) => {
    await page.goto(tokenPath("factory/bze1nobody/uzz"));
    await expect(page.getByRole("heading", { name: "Token not found" })).toBeVisible();
  });
});

test.describe("tokens on a phone", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("stacks the rows and keeps both pages within the screen", async ({ page }) => {
    await page.goto("/tokens");
    await expect(rows(page)).toHaveCount(6);
    await noHorizontalScroll(page);

    await page.goto(tokenPath(honey.denom));
    await expect(page.getByTestId("activity-item")).toHaveCount(7);
    await noHorizontalScroll(page);
  });
});
