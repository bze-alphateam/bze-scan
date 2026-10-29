import { describe, expect, it } from "vitest";

import { csvAmount, csvField, csvFileName, csvTime, toCsv } from "./csv";

describe("csvField", () => {
  it("leaves plain values as they are and writes booleans as yes/no", () => {
    expect(csvField("Sent to DEX")).toBe("Sent to DEX");
    expect(csvField(25120301)).toBe("25120301");
    expect(csvField(true)).toBe("yes");
    expect(csvField(false)).toBe("no");
    expect(csvField(null)).toBe("");
    expect(csvField(undefined)).toBe("");
  });

  it("quotes a comma, a quote or a line break, doubling the quotes", () => {
    expect(csvField("sold 200 BZE, bought 2.56 USDC")).toBe('"sold 200 BZE, bought 2.56 USDC"');
    expect(csvField('the "Honey" token')).toBe('"the ""Honey"" token"');
    expect(csvField("two\nlines")).toBe('"two\nlines"');
  });
});

describe("toCsv", () => {
  it("writes the header, then the rows, comma-delimited with CRLF line ends", () => {
    expect(toCsv(["Time (UTC)", "Amount"], [["2026-10-12 09:41:00", "-10"], ["2026-10-11 08:00:00", "2.5"]])).toBe(
      "Time (UTC),Amount\r\n2026-10-12 09:41:00,-10\r\n2026-10-11 08:00:00,2.5\r\n",
    );
  });
});

describe("csvTime", () => {
  it("writes UTC as YYYY-MM-DD HH:mm:ss", () => {
    expect(csvTime("2026-10-12T09:41:07Z")).toBe("2026-10-12 09:41:07");
    expect(csvTime("2026-01-02T23:04:05+02:00")).toBe("2026-01-02 21:04:05");
    expect(csvTime("not a time")).toBe("");
  });
});

describe("csvAmount", () => {
  it("shifts base units to display units exactly, with no grouping", () => {
    expect(csvAmount("1234500000", 6)).toBe("1234.5");
    expect(csvAmount("-200000000", 6)).toBe("-200");
    expect(csvAmount("1", 18)).toBe("0.000000000000000001");
    expect(csvAmount("123456789012345678901234567890", 6)).toBe("123456789012345678901234.56789");
    expect(csvAmount("42", 0)).toBe("42");
  });
});

describe("csvFileName", () => {
  it("names what, whose and when (UTC), keeping the name file-safe", () => {
    const now = new Date("2026-10-29T14:22:33Z");
    expect(csvFileName("transactions", "bze1m3kq7x", now)).toBe("transactions-bze1m3kq7x-20261029-142233.csv");
    expect(csvFileName("holders", "USDC/ibc", now)).toBe("holders-USDC_ibc-20261029-142233.csv");
  });
});
