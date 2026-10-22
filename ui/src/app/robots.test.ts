import { afterEach, describe, expect, it, vi } from "vitest";

const load = async (chainId?: string) => {
  vi.resetModules();
  if (chainId === undefined) vi.stubEnv("NEXT_PUBLIC_CHAIN_ID", "");
  else vi.stubEnv("NEXT_PUBLIC_CHAIN_ID", chainId);
  return (await import("./robots")).default();
};

describe("robots", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it("opens the mainnet overview pages and closes single blocks, transactions and their paged lists", async () => {
    const robots = await load("beezee-1");
    expect(robots.rules).toEqual({
      userAgent: "*",
      allow: "/",
      disallow: ["/blocks/", "/blocks?", "/txs/", "/txs?", "/search"],
    });
  });

  it("treats the default chain id as mainnet", async () => {
    const robots = await load();
    expect(robots.rules).toMatchObject({ allow: "/" });
  });

  it("closes a testnet explorer entirely", async () => {
    const robots = await load("beezee-testnet-1");
    expect(robots.rules).toEqual({ userAgent: "*", disallow: "/" });
  });
});
