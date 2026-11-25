import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { PoolPrice } from "@/api/pool-price";
import type { Token } from "@/api/types";
import { renderWithProviders } from "@/test/render";

import lpJson from "../../../test/fixtures/api/v1/tokens/ulp_factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney_ubze.json";

import { PoolPriceCard } from "./pool-price-card";

const lp = lpJson as unknown as Token;
const withPool = (over: Partial<PoolPrice> | null): Token => ({ ...lp, pool: over === null ? null : { ...(lp.pool as PoolPrice), ...over } });

describe("PoolPriceCard", () => {
  it("shows the price, the average with its drift and both warnings past the limits", () => {
    renderWithProviders(<PoolPriceCard t={lp} />);
    expect(screen.getByTestId("pool-price-now")).toHaveTextContent("2");
    expect(screen.getByTestId("pool-price")).toHaveTextContent("Recent average (half-life 20 blocks)");
    expect(screen.getByTestId("pool-price-avg")).toHaveTextContent("1.8");
    expect(screen.getByTestId("pool-price-drift")).toHaveTextContent("11.1% away");
    expect(screen.getByTestId("pool-status-fees")).toHaveTextContent("Fees paid in HONEY are charged in BZE for now");
    expect(screen.getByTestId("pool-status-module")).toHaveTextContent("burner moves through this pool are on hold: more than 3% away");
    expect(screen.getByTestId("pool-price-trade")).toHaveTextContent("Trade on the DEX");
  });

  it("says within limits", () => {
    renderWithProviders(<PoolPriceCard t={withPool({ drift_pct: "0.5", fees_in_bze: false, module_swaps_held: false })} />);
    expect(screen.getByTestId("pool-status-ok")).toHaveTextContent("Within limits.");
    expect(screen.queryByTestId("pool-status-fees")).not.toBeInTheDocument();
  });

  it("copes with a null average", () => {
    renderWithProviders(
      <PoolPriceCard t={withPool({ avg_price: null, avg_last_height: null, drift_pct: null, fees_in_bze: null, module_swaps_held: null })} />,
    );
    expect(screen.getByTestId("pool-price-avg")).toHaveTextContent("—");
    expect(screen.queryByTestId("pool-price-drift")).not.toBeInTheDocument();
    expect(screen.getByTestId("pool-status-unknown")).toHaveTextContent("no recent average");
  });

  it("explains itself in a pop-up", async () => {
    renderWithProviders(<PoolPriceCard t={lp} />);
    await userEvent.click(screen.getByTestId("pool-price-help"));
    expect(await screen.findByText(/charged in BZE\s+instead/)).toBeInTheDocument();
  });

  it("does not claim a BZE pool has no rule when its limits could not be read", () => {
    renderWithProviders(<PoolPriceCard t={withPool({ fees_in_bze: null, module_swaps_held: null })} />);
    expect(screen.getByTestId("pool-status-unchecked")).toHaveTextContent("could not be checked");
    expect(screen.queryByTestId("pool-status-norule")).not.toBeInTheDocument();
  });

  it("says no rule applies only to a pool without BZE", () => {
    const base = lp.pool as PoolPrice;
    const other = { ...base.reserves, quote: { ...base.reserves.quote, denom: "uother", symbol: "OTHER" } };
    renderWithProviders(<PoolPriceCard t={withPool({ reserves: other, fees_in_bze: null, module_swaps_held: null })} />);
    expect(screen.getByTestId("pool-status-norule")).toBeInTheDocument();
  });

  it("shows a chain-unit price without trailing zeros", () => {
    renderWithProviders(<PoolPriceCard t={withPool({ price: { chain: "2.000000000000000000", display: null } })} />);
    expect(screen.getByTestId("pool-price-now")).toHaveTextContent(/^2$/);
  });

  it("draws nothing for a token without a pool", () => {
    renderWithProviders(<PoolPriceCard t={withPool(null)} />);
    expect(screen.queryByTestId("pool-price")).not.toBeInTheDocument();
  });
});
