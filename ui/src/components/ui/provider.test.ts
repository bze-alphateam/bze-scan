import { describe, expect, it } from "vitest";

import { system } from "./provider";

const TOUCH_MEDIA = "@media (hover: none) and (pointer: coarse)";

/** Every declaration of a compiled CSS object, with the at-rules and selectors it is nested in. */
const compiledRules = (node: object, path: string[] = []): { path: string[]; prop: string; value: string }[] =>
  Object.entries(node).flatMap(([key, value]) =>
    typeof value === "object" && value !== null
      ? compiledRules(value, [...path, key])
      : [{ path, prop: key, value: String(value) }],
  );

describe("system", () => {
  it("uses 16px text in fields on touch devices only, so iOS Safari does not zoom on focus", () => {
    const rule = compiledRules(system.getGlobalCss()).find(
      ({ path, prop, value }) =>
        /font-?size/i.test(prop) && /^16px\s*!important$/.test(value) && path.includes(TOUCH_MEDIA),
    );

    expect(rule, "the 16px touch rule is missing from the compiled global CSS").toBeDefined();
    const selector = rule!.path.filter((part) => !part.startsWith("@")).join(" ");
    expect(selector).toMatch(/\binput\b/);
    expect(selector).toMatch(/\btextarea\b/);
    expect(selector).toMatch(/\bselect\b/);
  });
});
