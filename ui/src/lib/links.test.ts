import { existsSync } from "node:fs";
import { join } from "node:path";

import { afterEach, describe, expect, it, vi } from "vitest";

// env.ts reads NEXT_PUBLIC_* when it is first imported (next build inlines
// them), so every case stubs the environment and imports the modules afresh.
async function load() {
  vi.resetModules();
  const links = await import("./links");
  const account = await import("@/render/account");
  const dex = await import("@/render/messages/dex");
  const parameters = await import("./parameters");
  return { ...links, ...account, ...dex, ...parameters };
}

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("app links", () => {
  it("point at the mainnet apps by default", async () => {
    const { appLinks, externalLinks, stakeHref } = await load();
    expect(externalLinks.staking).toBe("https://staking.getbze.com");
    expect(appLinks.find((l) => l.label === "Communities")?.href).toBe("https://communities.getbze.com");
    expect(stakeHref("bzevaloper1x")).toBe("https://staking.getbze.com/?stake=bzevaloper1x");
  });

  it("follow the build env, so a testnet build links the testnet apps", async () => {
    vi.stubEnv("NEXT_PUBLIC_STAKING_URL", "https://testnet-staking.getbze.com/");
    vi.stubEnv("NEXT_PUBLIC_DEX_URL", "https://testnet-dex.getbze.com");
    vi.stubEnv("NEXT_PUBLIC_BURNER_URL", "https://testnet-burner.getbze.com");
    vi.stubEnv("NEXT_PUBLIC_FACTORY_URL", "https://testnet-factory.getbze.com");
    vi.stubEnv("NEXT_PUBLIC_COMMUNITIES_URL", "https://testnet-communities.getbze.com");
    const { appLinks, externalLinks, stakeHref, burnerRaffleHref, stakingVoteHref, dexAssetUrl, dexMarketUrl, PARAM_GROUPS } = await load();

    expect(appLinks.map((l) => l.href)).toEqual([
      "https://getbze.com",
      "https://testnet-staking.getbze.com",
      "https://testnet-dex.getbze.com",
      "https://testnet-burner.getbze.com",
      "https://testnet-factory.getbze.com",
      "https://testnet-communities.getbze.com",
      "https://docs.getbze.com",
    ]);
    expect(stakeHref("bzevaloper1x")).toBe("https://testnet-staking.getbze.com/?stake=bzevaloper1x");
    expect(burnerRaffleHref("factory/bze1x/uhoney")).toBe("https://testnet-burner.getbze.com/coin?coin=factory%2Fbze1x%2Fuhoney");
    expect(stakingVoteHref(47)).toBe("https://testnet-staking.getbze.com/governance/47");
    expect(dexAssetUrl("ubze")).toBe("https://testnet-dex.getbze.com/assets/details?denom=ubze");
    expect(dexMarketUrl("ubze/uvdl")).toBe("https://testnet-dex.getbze.com/exchange/market?id=ubze%2Fuvdl");
    expect(PARAM_GROUPS.find((g) => g.group === "burner")?.see).toEqual({
      label: "testnet-burner.getbze.com",
      href: externalLinks.burner,
    });
  });
});

describe("app identity", () => {
  it("gives every app its icons, from files the explorer serves, and its accent", async () => {
    const { appIdentity } = await load();
    const { env } = await import("./env");
    expect(Object.keys(appIdentity).sort()).toEqual(Object.keys(env.apps).sort());
    for (const [app, id] of Object.entries(appIdentity)) {
      const files = [id.icon.light, id.icon.dark, id.iconSmall.light, id.iconSmall.dark];
      expect(files).toEqual([
        `/images/apps/${app}/icon.svg`,
        `/images/apps/${app}/icon-on-dark.svg`,
        `/images/apps/${app}/icon-small.svg`,
        `/images/apps/${app}/icon-small-on-dark.svg`,
      ]);
      for (const f of files) expect(existsSync(join(process.cwd(), "public", f)), f).toBe(true);
      expect(id.accent.light).toMatch(/^#[0-9A-F]{6}$/);
      expect(id.accent.dark).toMatch(/^#[0-9A-F]{6}$/);
    }
    // Ink on light, the light end on dark (Official-BZEdge-Graphics, beezee/apps).
    expect(appIdentity.dex.accent).toEqual({ light: "#0E74AD", dark: "#12C1E1" });
    expect(appIdentity.factory.accent).toEqual({ light: "#9A6A00", dark: "#FFC933" });
  });

  it("puts an icon on every app link; Website and Docs keep the explorer's accent", async () => {
    const { appLinks, appAccent, appIdentity } = await load();
    expect(appLinks.map((l) => l.icon)).toEqual(["website", "staking", "dex", "burner", "factory", "communities", "docs"]);
    expect(appAccent("staking")).toBe(appIdentity.staking.accent);
    expect(appAccent("website")).toBeNull();
    expect(appAccent("docs")).toBeNull();
  });
});
