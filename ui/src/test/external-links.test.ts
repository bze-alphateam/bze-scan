import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

import { describe, expect, it } from "vitest";

// Every link that leaves the explorer goes through AppLink (a BZE app) or
// ExternalLink (anything else). Only they may open a new tab themselves. The
// Apps menu, the home page's Go-to tiles and the footer keep their own design
// as navigation and open in the same tab.
const NEW_TAB_ALLOWED = new Set(["components/layout/app-link.tsx", "components/ui/external-link.tsx"]);
const NEW_TAB = /target=\{?\s*["'`]_blank/;

const SRC = join(__dirname, "..");

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : [];
  });
}

describe("external links", () => {
  const files = sources(SRC).map((path) => ({ file: relative(SRC, path), text: readFileSync(path, "utf8") }));

  it("open a new tab only through AppLink or ExternalLink", () => {
    const offenders = files.filter((f) => NEW_TAB.test(f.text) && !NEW_TAB_ALLOWED.has(f.file)).map((f) => f.file);
    expect(offenders).toEqual([]);
  });

  it("never carry a typed arrow: the icon is the only external mark", () => {
    const offenders = files.filter((f) => f.text.includes("↗")).map((f) => f.file);
    expect(offenders).toEqual([]);
  });
});
