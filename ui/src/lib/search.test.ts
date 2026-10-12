import { describe, expect, it } from "vitest";

import { classifyQuery, orderResults, resultHref } from "./search";

const ACCOUNT = "bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4";
const VALOPER = "bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c";
const HASH = "3F9A6B2C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708192A3B4C5D0C21E";

describe("classifyQuery", () => {
  it("sends a height to its block", () => {
    expect(classifyQuery(" 25120440 ")).toEqual({ kind: "go", href: "/blocks/25120440" });
    expect(classifyQuery("007")).toEqual({ kind: "go", href: "/blocks/7" });
  });

  it("sends a hash in either case to its transaction, upper-cased", () => {
    expect(classifyQuery(HASH.toLowerCase())).toEqual({ kind: "go", href: `/txs/${HASH}` });
    expect(classifyQuery(HASH)).toEqual({ kind: "go", href: `/txs/${HASH}` });
  });

  it("sends an account and a validator to their pages", () => {
    expect(classifyQuery(ACCOUNT)).toEqual({ kind: "go", href: `/accounts/${ACCOUNT}`, account: ACCOUNT });
    expect(classifyQuery(ACCOUNT.toUpperCase())).toEqual({ kind: "go", href: `/accounts/${ACCOUNT}`, account: ACCOUNT });
    expect(classifyQuery(VALOPER)).toEqual({ kind: "go", href: `/validators/${VALOPER}` });
  });

  it("searches any other text of two characters or more", () => {
    expect(classifyQuery("Honey")).toEqual({ kind: "search", q: "Honey" });
    expect(classifyQuery("bze1abc")).toEqual({ kind: "search", q: "bze1abc" });
    expect(classifyQuery("0")).toEqual({ kind: "none" });
    expect(classifyQuery("00")).toEqual({ kind: "search", q: "00" });
    expect(classifyQuery(HASH.slice(1))).toEqual({ kind: "search", q: HASH.slice(1) });
    expect(classifyQuery(`${ACCOUNT.slice(0, 10)}${ACCOUNT.slice(10).toUpperCase()}`).kind).toBe("search");
  });

  it("does nothing with one character or blanks", () => {
    expect(classifyQuery("h")).toEqual({ kind: "none" });
    expect(classifyQuery("   ")).toEqual({ kind: "none" });
  });
});

describe("search results", () => {
  it("links each result to its page and puts validators first", () => {
    const results = [
      { type: "account", id: ACCOUNT, label: "Burner" },
      { type: "validator", id: VALOPER, label: "Honeycomb" },
    ];
    expect(orderResults(results).map((r) => r.type)).toEqual(["validator", "account"]);
    expect(results.map(resultHref)).toEqual([`/accounts/${ACCOUNT}`, `/validators/${VALOPER}`]);
    expect(resultHref({ type: "block", id: "5", label: "5" })).toBe("/blocks/5");
    expect(resultHref({ type: "transaction", id: HASH, label: HASH })).toBe(`/txs/${HASH}`);
  });
});
