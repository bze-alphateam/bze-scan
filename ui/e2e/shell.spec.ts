import { expect, test } from "@playwright/test";

test.describe("site shell", () => {
  test("renders the hero, the footer and no horizontal scroll", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { level: 1, name: "Explore the BeeZee network" })).toBeVisible();
    await expect(page.getByText("The explorer is being built")).toBeVisible();
    await expect(page.getByTestId("tile-skeleton")).toHaveCount(6);

    const footer = page.getByRole("contentinfo");
    await expect(footer.getByText("BZE Explorer · chain beezee-1")).toBeVisible();
    await expect(footer.getByRole("link", { name: "Ping.pub explorer" })).toHaveAttribute(
      "href",
      "https://explorer.getbze.com/beezee",
    );
    await expect(footer.getByRole("link", { name: "Parameters" })).toHaveAttribute("href", "/parameters");
    await expect(footer.getByRole("button", { name: /^Explorer status/ })).toBeVisible();

    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });

  test("submits a search to /search", async ({ page }) => {
    await page.goto("/");
    // The hero's box is on every viewport.
    const box = page.getByRole("main").getByRole("searchbox", { name: "Search the explorer" });
    await box.fill("bze1abc");
    await box.press("Enter");
    await expect(page).toHaveURL(/\/search\?q=bze1abc$/);
    // The search page lands with its ticket; until then it is a 404.
    await expect(page.getByRole("heading", { name: "Nothing here" })).toBeVisible();
  });
});

test.describe("desktop header", () => {
  test.skip(({ isMobile }) => isMobile, "desktop layout");

  test("shows the nav, the search box, the Apps menu and the theme toggle", async ({ page }) => {
    await page.goto("/");
    const header = page.getByRole("banner");
    const nav = header.getByRole("navigation", { name: "Explorer" });
    await expect(nav.getByRole("link")).toHaveText(["Blocks", "Transactions", "Validators", "Tokens", "Proposals", "Parameters"]);
    await expect(header.getByRole("searchbox", { name: "Search the explorer" })).toBeVisible();
    await expect(header.getByText("Mainnet")).toBeVisible();
    await expect(header.getByRole("button", { name: "Switch to light mode" })).toBeVisible();
    await expect(header.getByRole("button", { name: "Open menu" })).toBeHidden();

    await header.getByRole("button", { name: "Apps" }).click();
    await expect(page.getByRole("menuitem", { name: "DEX" })).toHaveAttribute("href", "https://dex.getbze.com");
    await expect(page.getByRole("menuitem")).toHaveCount(7);
  });
});

test.describe("phone header", () => {
  test.skip(({ isMobile }) => !isMobile, "phone layout");

  test("collapses to the logo, a search icon and the menu", async ({ page }) => {
    await page.goto("/");
    const header = page.getByRole("banner");
    await expect(header.getByRole("link", { name: "BZE Explorer home" })).toBeVisible();
    await expect(header.getByRole("navigation", { name: "Explorer" })).toBeHidden();
    await expect(header.getByRole("searchbox")).toBeHidden();

    await header.getByRole("button", { name: "Open search" }).click();
    await expect(header.getByRole("searchbox", { name: "Search the explorer" })).toBeFocused();
  });

  test("opens the drawer and navigates from it", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: "Open menu" }).click();

    const drawer = page.getByRole("dialog");
    await expect(drawer.getByRole("link", { name: "DEX" })).toHaveAttribute("href", "https://dex.getbze.com");
    await drawer.getByRole("link", { name: "Validators" }).click();

    await expect(page).toHaveURL(/\/validators$/);
    await expect(page.getByRole("dialog")).toBeHidden();
  });
});
