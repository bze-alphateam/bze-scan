import { expect, type Page } from "@playwright/test";

/** Fails naming the innermost elements that reach past the screen's right edge. */
export async function noHorizontalScroll(page: Page) {
  const { overflow, culprits } = await page.evaluate(() => {
    const width = document.documentElement.clientWidth;
    const wide = [...document.body.querySelectorAll<HTMLElement>("*")].filter(
      (el) => el.getBoundingClientRect().right > width + 0.5,
    );
    const innermost = wide.filter((el) => !wide.some((other) => other !== el && el.contains(other)));
    return {
      overflow: document.documentElement.scrollWidth - width,
      culprits: innermost.slice(0, 5).map((el) => {
        const r = el.getBoundingClientRect();
        const id = el.dataset.testid ? `[data-testid=${el.dataset.testid}]` : "";
        return `${el.tagName.toLowerCase()}${id} right=${Math.round(r.right)} "${(el.textContent ?? "").trim().slice(0, 40)}"`;
      }),
    };
  });
  expect(overflow, `wider than the screen: ${culprits.join("; ")}`).toBeLessThanOrEqual(0);
}
