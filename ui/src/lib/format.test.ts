import { describe, expect, it } from "vitest";

import {
  avatarHue,
  formatAmount,
  formatCount,
  formatDateTime,
  formatPct,
  formatUsd,
  initials,
  relativeTime,
  shortAddress,
  shortHash,
} from "./format";

describe("formatAmount", () => {
  it("shifts by the exponent, groups thousands and trims trailing zeros", () => {
    expect(formatAmount("1234500000", 6, "BZE")).toBe("1,234.5 BZE");
    expect(formatAmount("1000000", 6, "BZE")).toBe("1 BZE");
    expect(formatAmount("1", 6, "BZE")).toBe("0.000001 BZE");
    expect(formatAmount("0", 6, "BZE")).toBe("0 BZE");
  });

  it("keeps at most `exponent` decimals", () => {
    expect(formatAmount("123", 0, "TKN")).toBe("123 TKN");
    expect(formatAmount("1234567891234", 6)).toBe("1,234,567.891234");
  });

  it("handles amounts beyond a double's precision", () => {
    expect(formatAmount("123456789012345678901234567", 18, "X")).toBe("123,456,789.012345678901234567 X");
  });

  it("shortens large values in compact form", () => {
    expect(formatAmount("14300000000000", 6, "BZE", { compact: true })).toBe("14.3M BZE");
    expect(formatAmount("2500000000", 6, "BZE", { compact: true })).toBe("2.5K BZE");
    expect(formatAmount("7000000000000000000", 6, "BZE", { compact: true })).toBe("7T BZE");
    expect(formatAmount("12345678", 6, "BZE", { compact: true })).toBe("12.34 BZE");
    expect(formatAmount("1234", 6, "BZE", { compact: true })).toBe("0.001234 BZE");
  });

  it("returns the input when it is not a number", () => {
    expect(formatAmount("abc", 6)).toBe("abc");
  });
});

describe("formatUsd", () => {
  it("uses two decimals and thousands separators", () => {
    expect(formatUsd("11.5")).toBe("≈ $11.50");
    expect(formatUsd(1234567.891)).toBe("≈ $1,234,567.89");
    expect(formatUsd(0)).toBe("≈ $0.00");
  });

  it("uses four significant digits under one cent", () => {
    expect(formatUsd("0.00123456")).toBe("≈ $0.001235");
    expect(formatUsd("0.009")).toBe("≈ $0.009");
  });
});

describe("formatPct and formatCount", () => {
  it("formats a percentage with one decimal", () => {
    expect(formatPct("12.34567")).toBe("12.3%");
    expect(formatPct(100)).toBe("100.0%");
    expect(formatPct("0.04")).toBe("0.0%");
  });

  it("groups counts", () => {
    expect(formatCount(1234567)).toBe("1,234,567");
  });
});

describe("shortHash and shortAddress", () => {
  it("keeps the first and last four characters of a hash, upper case", () => {
    expect(shortHash("3f9a0000000000000000000000000000000000000000000000000000000c21e")).toBe("3F9A…C21E");
    expect(shortHash("abc")).toBe("ABC");
  });

  it("keeps the prefix, three characters and the last four of an address", () => {
    expect(shortAddress("bze1q8f7z6x5c4v3d2n0mlkjhgfdsapzxu4a")).toBe("bze1q8f…xu4a");
    expect(shortAddress("bzevaloper1acdefghjklmnpqrstuvwxyz0023")).toBe("bzevaloper1acd…0023");
    expect(shortAddress("bze1abcdefgh")).toBe("bze1abcdefgh");
  });
});

describe("times", () => {
  const now = new Date("2026-10-06T14:32:11Z");

  it("formats relative times", () => {
    expect(relativeTime("2026-10-06T14:31:59Z", now)).toBe("12 seconds ago");
    expect(relativeTime("2026-10-06T14:30:11Z", now)).toBe("2 minutes ago");
    expect(relativeTime("2026-10-06T11:32:11Z", now)).toBe("3 hours ago");
    expect(relativeTime("2026-10-04T14:32:11Z", now)).toBe("2 days ago");
    expect(relativeTime("2026-10-06T14:37:11Z", now)).toBe("in 5 minutes");
    expect(relativeTime(now, now)).toBe("just now");
  });

  it("formats absolute times in UTC", () => {
    expect(formatDateTime("2026-10-06T14:32:11Z")).toBe("Oct 6, 2026, 14:32:11 UTC");
    expect(formatDateTime("2026-01-09T03:04:05.678+02:00")).toBe("Jan 9, 2026, 01:04:05 UTC");
    expect(formatDateTime("not a date")).toBe("");
  });
});

describe("avatars", () => {
  it("takes initials from a moniker", () => {
    expect(initials("Thamar Node")).toBe("TN");
    expect(initials("cosmostation")).toBe("CO");
    expect(initials("  ")).toBe("?");
  });

  it("takes the two characters after an address prefix", () => {
    expect(initials("bze1q8f7z6x5c4v3d2n0m")).toBe("Q8");
    expect(initials("bzevaloper1xyzwv2t3")).toBe("XY");
  });

  it("derives a stable hue", () => {
    expect(avatarHue("Thamar")).toBe(avatarHue("Thamar"));
    expect(avatarHue("Thamar")).not.toBe(avatarHue("Thamaz"));
    expect(avatarHue("x")).toBeGreaterThanOrEqual(0);
    expect(avatarHue("a very long moniker that overflows")).toBeLessThan(360);
  });
});
