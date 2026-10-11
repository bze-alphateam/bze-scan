import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Transfer } from "@/api/types";
import { renderWithProviders } from "@/test/render";

import { deltaText, isSimple, netFor } from "./net";
import { flowSummary, StepList } from "./step-list";

const ME = "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw";
const YOU = "bze1m3kq7xzx2c0w6wyd5ys8xhzm8wxdq0p9fz9ds4";
const FEES = "bze17xpfvakm2amg962yls6f84z3kell8c5l6s5ye9";
const DEX = "bze1x7ym3n6d9kjwz0q4u5r2a8l4tqs9vfmyh2gz3e";

function t(seq: number, msg_index: number | null, sender: string | null, recipient: string | null, amount: string, extra: Partial<Transfer> = {}): Transfer {
  return {
    seq,
    msg_index,
    kind: "transfer",
    sender,
    sender_label: sender === FEES ? { name: "Fee collector", kind: "module" } : null,
    recipient,
    recipient_label: recipient === FEES ? { name: "Fee collector", kind: "module" } : recipient === DEX ? { name: "DEX", kind: "module" } : null,
    denom: "ubze",
    amount,
    symbol: "BZE",
    exponent: 6,
    ...extra,
  };
}

const send = [t(1, 0, ME, YOU, "10000000"), t(0, null, ME, FEES, "2000")];

describe("isSimple", () => {
  it("collapses one action with at most two transfers", () => {
    expect(isSimple(1, 2)).toBe(true);
    expect(isSimple(1, 3)).toBe(false);
    expect(isSimple(2, 2)).toBe(false);
    expect(isSimple(1, 0)).toBe(true);
  });
});

describe("netFor", () => {
  it("sums received minus sent per denom, the fee on its own", () => {
    const swap = [
      t(0, null, ME, FEES, "2000"),
      t(1, 0, ME, DEX, "10000000"),
      t(2, 0, DEX, ME, "3000000", { denom: "uusdc", symbol: "USDC" }),
      t(3, 1, DEX, ME, "500000"),
      t(4, 1, YOU, DEX, "1"),
    ];
    const net = netFor(ME, swap);
    expect(net.deltas.map(deltaText)).toEqual(["-9.5 BZE", "+3 USDC"]);
    expect(net.fees.map((f) => f.amount)).toEqual(["2000"]);
  });

  it("drops a denom that nets to zero and keeps an unknown denom raw", () => {
    const net = netFor(ME, [
      t(1, 0, ME, DEX, "5"),
      t(2, 0, DEX, ME, "5"),
      t(3, 0, DEX, ME, "7", { denom: "factory/x/uhoney", symbol: null, exponent: null }),
    ]);
    expect(net.deltas.map(deltaText)).toEqual(["+7 factory/x/uhoney"]);
    expect(net.fees).toEqual([]);
  });
});

describe("StepList", () => {
  it("summarises a simple flow in one line and opens on demand, in seq order", async () => {
    expect(flowSummary(send.slice().sort((a, b) => a.seq - b.seq))).toBe(
      "2 transfers · 0.002 BZE fee to Fee collector · 10 BZE to bze1m3k…9ds4",
    );
    renderWithProviders(<StepList transfers={send} reasonOf={() => "sent"} collapsed netOf={null} />);
    expect(screen.queryAllByTestId("flow-step")).toHaveLength(0);
    await userEvent.click(screen.getByRole("button", { name: /Show steps/ }));
    const steps = screen.getAllByTestId("flow-step");
    expect(steps).toHaveLength(2);
    expect(steps[0]).toHaveTextContent("transaction fee");
    expect(steps[1]).toHaveTextContent("sent");
  });

  it("opens a complex flow with the signer's net", () => {
    const flow = [...send, t(2, 1, YOU, ME, "1000000")];
    renderWithProviders(<StepList transfers={flow} reasonOf={() => "swap"} collapsed={false} netOf={ME} />);
    expect(screen.getAllByTestId("flow-step")).toHaveLength(3);
    const net = screen.getByTestId("net-box");
    expect(net).toHaveTextContent("Net for bze18uf…9zhw");
    expect(net).toHaveTextContent("-9 BZE");
    expect(net).toHaveTextContent("-0.002 BZE fee");
  });

  it("says why a mint and a burn happened", () => {
    const flow = [t(0, 0, null, ME, "1", { kind: "mint" }), t(1, 0, ME, null, "1", { kind: "burn" })];
    renderWithProviders(<StepList transfers={flow} reasonOf={() => "other"} collapsed={false} />);
    const steps = screen.getAllByTestId("flow-step");
    expect(steps[0]).toHaveTextContent("the chain");
    expect(steps[0]).toHaveTextContent("created by the chain");
    expect(steps[1]).toHaveTextContent("burned");
  });

  it("says when no funds moved", () => {
    renderWithProviders(<StepList transfers={[]} reasonOf={() => ""} collapsed />);
    expect(screen.getByText("No funds moved.")).toBeInTheDocument();
  });
});
