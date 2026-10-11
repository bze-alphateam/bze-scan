import { expect, test } from "@playwright/test";

test("dark by default, and the choice survives a reload", async ({ page, isMobile }) => {
  await page.goto("/");
  const html = page.locator("html");
  await expect(html).toHaveClass(/\bdark\b/);

  // On a phone the toggle lives in the menu drawer.
  if (isMobile) {
    await page.getByRole("button", { name: "Open menu" }).click();
  }
  await page.getByRole("button", { name: "Switch to light mode" }).click();
  await expect(html).toHaveClass(/\blight\b/);

  await page.reload();
  await expect(html).toHaveClass(/\blight\b/);
  await expect(html).not.toHaveClass(/\bdark\b/);
});
