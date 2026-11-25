import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Token } from "@/api/types";
import { renderWithProviders } from "@/test/render";

import usdcJson from "../../../test/fixtures/api/v1/tokens/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4.json";

import { TokenLimits } from "./token-limits";

const usdc = usdcJson as Token;

describe("TokenLimits", () => {
  it("shows a row per channel with its two bars and a link to /ibc", () => {
    renderWithProviders(<TokenLimits t={usdc} />);
    const rows = screen.getAllByTestId("token-limit-row");
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveTextContent("Noble");
    expect(within(rows[0]).getByTestId("limit-out")).toHaveTextContent("No practical limit");
    expect(within(rows[0]).getByTestId("limit-in")).toHaveTextContent("not allowed on this route");
    expect(screen.getByRole("link", { name: /All cross-chain limits/ })).toHaveAttribute("href", "/ibc");
  });

  it("is hidden when the token has no limits, or the answer predates them", () => {
    const { rerender } = renderWithProviders(<TokenLimits t={{ ...usdc, rate_limits: [] }} />);
    expect(screen.queryByTestId("token-limits")).not.toBeInTheDocument();
    const old: Partial<Token> = { ...usdc };
    delete old.rate_limits;
    rerender(<TokenLimits t={old as Token} />);
    expect(screen.queryByTestId("token-limits")).not.toBeInTheDocument();
  });
});
