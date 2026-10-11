import { describe, expect, it } from "vitest";

import { failureReason } from "./errors";

const symbolOf = (denom: string) => (denom === "ubze" ? "BZE" : null);

describe("failureReason", () => {
  it("names the coin that was short, from the log", () => {
    const tx = { codespace: "sdk", code: 5, error_log: "spendable balance 1ubze is smaller than 100ubze: insufficient funds" };
    expect(failureReason(tx, symbolOf)).toBe("Not enough BZE for the amount plus the fee");
  });

  it("falls back to the fee's denom, then the raw denom", () => {
    expect(failureReason({ codespace: "sdk", code: 5, error_log: null }, symbolOf, "ubze")).toBe(
      "Not enough BZE for the amount plus the fee",
    );
    expect(failureReason({ codespace: "sdk", code: 5, error_log: "is smaller than 9uatom" }, symbolOf)).toBe(
      "Not enough uatom for the amount plus the fee",
    );
    expect(failureReason({ codespace: "sdk", code: 5, error_log: null })).toBe("Not enough funds for the amount plus the fee");
  });

  it.each([
    [11, /^Ran out of gas/],
    [13, /fee was too low/],
    [19, /^Already sent/],
    [32, "Sent out of order, try again"],
  ])("maps sdk code %i", (code, want) => {
    expect(failureReason({ codespace: "sdk", code, error_log: "raw log" })).toMatch(want);
  });

  it("shows another code's log verbatim", () => {
    const log = "failed to execute message; message index: 0: order price too low";
    expect(failureReason({ codespace: "tradebin", code: 5, error_log: log })).toBe(log);
  });

  it("names the code when there is no log", () => {
    expect(failureReason({ codespace: "wasm", code: 7, error_log: null })).toBe("Failed with code 7 (wasm)");
  });
});
