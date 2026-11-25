import { describe, expect, it } from "vitest";

import { failureReason } from "./errors";

const fail = (codespace: string | null, code: number, error_log: string | null = "raw log") => failureReason({ codespace, code, error_log });

describe("failureReason of chain v8.3.0", () => {
  it.each([
    ["tokenfactory", 1112, "Minting of this token is turned off for good."],
    ["tradebin", 4018, "The pool's price moved too far from its recent average; try again in a few blocks."],
    ["dao", 1133, "This kind of action is not allowed in a DAO execution."],
    ["dao", 1134, "The DAO's actions are nested too deep."],
    ["dao", 1135, "This admin nomination is out of date: control of the nominating DAO changed since it was made."],
  ])("maps %s code %i", (codespace, code, want) => {
    expect(fail(codespace, code)).toBe(want);
  });

  it("explains a message type the circuit breaker switched off", () => {
    expect(fail("undefined", 1, "tx type not allowed")).toBe("This action is switched off by an emergency measure.");
  });

  it("shows the log for a code it does not know", () => {
    expect(fail("tradebin", 4999)).toBe("raw log");
    expect(fail("dao", 9999)).toBe("raw log");
  });
});
