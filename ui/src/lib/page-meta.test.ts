import type { Metadata } from "next";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as account from "@/app/accounts/[address]/page";
import * as block from "@/app/blocks/[height]/page";
import * as futureBlock from "@/app/blocks/future/page";
import * as blocks from "@/app/blocks/page";
import * as home from "@/app/page";
import * as parameters from "@/app/parameters/page";
import * as proposal from "@/app/proposals/[id]/page";
import * as proposals from "@/app/proposals/page";
import * as search from "@/app/search/page";
import * as token from "@/app/tokens/[...denom]/page";
import * as tokens from "@/app/tokens/page";
import * as tx from "@/app/txs/[hash]/page";
import * as txs from "@/app/txs/page";
import * as validator from "@/app/validators/[operator]/page";
import * as validators from "@/app/validators/page";

import { OG_IMAGE, SITE_NAME, siteMetadata } from "./page-meta";

const ADDRESS = "bze1" + "q".repeat(38);
const OPERATOR = "bzevaloper1" + "q".repeat(38);
const HASH = "A".repeat(64);
const params = <T,>(p: T) => ({ params: Promise.resolve(p), searchParams: Promise.resolve({}) });

// The API answers a transaction and fails everything else, so pages take both their found and fallback paths.
let txStatus = 200;
beforeEach(() => {
  txStatus = 200;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.includes("/v1/txs/")
        ? new Response(JSON.stringify({ hash: HASH, success: true, messages: [{ type_url: "/x.y.MsgZ", value: {} }], transfers: [] }), {
            status: txStatus,
          })
        : new Response("{}", { status: 500 }),
    ),
  );
});
afterEach(() => vi.unstubAllGlobals());

/** What Next.js renders: a page's `openGraph` / `twitter` replaces the layout's whole object. */
const resolve = (page: Metadata): Metadata => ({ ...siteMetadata, ...page });

const routes: [string, () => Metadata | Promise<Metadata>][] = [
  ["/", () => home.metadata],
  ["/blocks", () => blocks.metadata],
  ["/blocks/future", () => futureBlock.metadata],
  ["/blocks/[height]", () => block.generateMetadata(params({ height: "100" }))],
  ["/txs", () => txs.metadata],
  ["/txs/[hash]", () => tx.generateMetadata(params({ hash: HASH }))],
  ["/txs/[hash] pending", () => ((txStatus = 404), tx.generateMetadata(params({ hash: HASH })))],
  ["/accounts/[address]", () => account.generateMetadata(params({ address: ADDRESS }) as never)],
  ["/validators", () => validators.metadata],
  ["/validators/[operator]", () => validator.generateMetadata(params({ operator: OPERATOR }))],
  ["/tokens", () => tokens.metadata],
  ["/tokens/[...denom]", () => token.generateMetadata(params({ denom: ["ubze"] }))],
  ["/proposals", () => proposals.metadata],
  ["/proposals/[id]", () => proposal.generateMetadata(params({ id: "8" }))],
  ["/parameters", () => parameters.generateMetadata()],
  ["/search", () => search.generateMetadata(params({}) as never)],
];

describe("page metadata", () => {
  it.each(routes)("%s shares the explorer image on a large card", async (_, load) => {
    const meta = resolve(await load());
    expect(meta.openGraph).toMatchObject({ siteName: SITE_NAME, type: "website", images: [OG_IMAGE] });
    expect(meta.twitter).toMatchObject({ card: "summary_large_image", images: [OG_IMAGE] });
  });

  it("keeps each page's own title in its share tags", () => {
    const meta = blocks.metadata;
    expect(meta.openGraph?.title).toBe("Blocks");
    expect(meta.twitter?.title).toBe("Blocks");
  });
});
