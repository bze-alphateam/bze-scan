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
    const { appLinks, externalLinks, stakeHref, dexAssetUrl, dexMarketUrl, PARAM_GROUPS } = await load();

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
    expect(dexAssetUrl("ubze")).toBe("https://testnet-dex.getbze.com/assets/details?denom=ubze");
    expect(dexMarketUrl("ubze/uvdl")).toBe("https://testnet-dex.getbze.com/exchange/market?id=ubze%2Fuvdl");
    expect(PARAM_GROUPS.find((g) => g.group === "burner")?.see).toEqual({
      label: "testnet-burner.getbze.com",
      href: externalLinks.burner,
    });
  });
});
