import { expect, test, type Page } from "@playwright/test";

import { noHorizontalScroll } from "./layout";
import { mockApi } from "./mock-api";

import honey from "../test/fixtures/api/v1/tokens/factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney.json";
import usdc from "../test/fixtures/api/v1/tokens/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4.json";
import atone from "../test/fixtures/api/v1/tokens/ibc/E7CF15949EE673E0BD3F5AA7D711DD0A6C22992E7E5714A174DCEA289EB696A1.json";

// The mock API answers /tokens from test/fixtures/api/v1/tokens.json (every
// kind; the query is ignored, so the chips filter on the page) and a token's
// routes from tokens/<denom>.json and tokens/<denom>/{holders,transfers}.json,
// the URL-encoded denom decoded into folders.
const rows = (page: Page) => page.getByTestId("token-list-row");
const tokenPath = (denom: string) => `/tokens/${encodeURIComponent(denom)}`;

test.describe("tokens list", () => {
  test("renders the tiles, filters by chip and name, and links to a token", async ({ page, isMobile }) => {
    await page.goto("/tokens");
    await expect(page).toHaveTitle("Tokens · BZE Explorer");
    await expect(page.getByText("Every token on BZE. Prices come from the BZE DEX, else CoinGecko.")).toBeVisible();
    await expect(page.getByTestId("tile")).toHaveCount(4);
    await expect(page.getByTestId("tile").first()).toContainText("1 native · 2 Factory · 2 cross-chain");
    await expect(page.getByTestId("tile").nth(1)).toContainText("3");
    await expect(rows(page)).toHaveCount(6);
    // A priced token shows its price and, for a DEX price, its DEX link (a desktop column); an unpriced one says so.
    await expect(rows(page).nth(1)).toContainText("$0.002");
    await expect(rows(page).nth(1).getByRole("link", { name: "DEX" })).toBeVisible({ visible: !isMobile });
    await expect(rows(page).nth(2)).toContainText("— no price");
    await expect(rows(page).nth(3)).toContainText("Halted on the DEX");
    await expect(rows(page).nth(3).getByRole("link", { name: "DEX" })).toHaveCount(0);
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
    // The DEX is a BZE app: a ghost AppLink after the verb, in a new tab.
    await expect(page.getByTestId("token-trade")).toHaveText("Trade on DEX");
    const dex = page.getByTestId("token-trade").getByRole("link", { name: "DEX" });
    await expect(dex).toHaveAttribute("data-testid", "app-link");
    await expect(dex).toHaveAttribute("target", "_blank");
    // The price names its source, with the rule as a tooltip.
    await expect(page.getByTestId("token-tiles")).toContainText("$0.002");
    await expect(page.getByTestId("price-source")).toHaveText("via DEX");
    await expect(page.getByTestId("price-source")).toHaveAttribute("title", /Spot price of its USDC or BZE pool/);
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

  test("a cross-chain token shows its origin chain and the way it came", async ({ page }) => {
    await page.goto(tokenPath(usdc.denom));
    await expect(page.getByTestId("token-chips")).toContainText("Cross-chainNoble");
    await expect(page.getByTestId("ibc-connection")).toContainText("Openand active");
    await expect(page.getByTestId("ibc-route")).toContainText("direct · 1 hop");
    await expect(page.getByTestId("route-stop")).toHaveCount(2);
    await expect(page.getByTestId("hops-callout")).toHaveCount(0);
    await expect(page.getByTestId("supply-box")).toContainText("USDC is minted on Noble");
    await expect(page.getByTestId("token-about")).toContainText("Base denom thereuusdc");
    await expect(page.getByText("No activity indexed yet.")).toBeVisible();
  });

  test("a token that came through another chain says it is not the direct one", async ({ page }) => {
    await page.goto(tokenPath(atone.denom));
    await expect(page.getByRole("heading", { level: 1, name: "ATONE" })).toBeVisible();
    await expect(page.getByTestId("token-chips")).toContainText("Cross-chainfrom AtomOnevia Osmosis2 hops");
    await expect(page.getByTestId("hops-callout")).toContainText("Not the same token as ATONE sent straight from AtomOne");
    await expect(page.getByTestId("ibc-route")).toContainText("2 hops · not direct");
    await expect(page.getByTestId("route-stop")).toHaveText([/AtomOne.*minted here/i, /Osmosis.*passed through/i, /BZE.*this token/i]);
    await expect(page.getByTestId("token-about")).toContainText("Path on Osmosis");
    await expect(page.getByText("No activity indexed yet.")).toBeVisible();
    await noHorizontalScroll(page);
  });

  test("a CoinGecko-priced token says so and has no DEX link; an unpriced one shows no source", async ({ page }) => {
    await mockApi(page, {
      [`v1/tokens/${encodeURIComponent(atone.denom)}`]: { ...atone, price_usd: "1.96", price_source: "coingecko" },
    });
    await page.goto(tokenPath(atone.denom));
    await expect(page.getByRole("heading", { level: 1, name: "ATONE" })).toBeVisible();
    await expect(page.getByTestId("token-tiles")).toContainText("$1.96");
    await expect(page.getByTestId("price-source")).toHaveText("via CoinGecko");
    await expect(page.getByTestId("token-trade")).toHaveCount(0);

    await page.unroute("**/api/v1/**");
    await page.goto(tokenPath(atone.denom));
    await expect(page.getByRole("heading", { level: 1, name: "ATONE" })).toBeVisible();
    await expect(page.getByTestId("token-tiles")).toContainText("no price");
    await expect(page.getByTestId("price-source")).toHaveCount(0);
  });

  test("an unknown token answers not found", async ({ page }) => {
    await page.goto(tokenPath("factory/bze1nobody/uzz"));
    await expect(page.getByRole("heading", { name: "Token not found" })).toBeVisible();
    await expect(page.getByRole("navigation", { name: "Breadcrumb" })).toContainText("Tokens");
    await expect(page.getByRole("heading", { level: 1, name: "Token" })).toBeVisible();
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

    // The route stacks: each chain under the one before.
    await page.goto(tokenPath(atone.denom));
    const stops = page.getByTestId("route-stop");
    await expect(stops).toHaveCount(3);
    const [first, second] = [await stops.nth(0).boundingBox(), await stops.nth(1).boundingBox()];
    expect(second!.y).toBeGreaterThan(first!.y + first!.height);
    await noHorizontalScroll(page);
  });
});
