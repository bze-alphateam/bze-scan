import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Stats } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import stats from "../../../test/fixtures/api/v1/stats.json";

import { NetworkStrip } from "./network-strip";

const strip = () => screen.getByRole("complementary", { name: "Network summary" });
const item = (label: string) => strip().querySelector(`[data-item="${label}"]`);

describe("NetworkStrip", () => {
  it("shows the network, the price and every figure from /stats", async () => {
    renderWithProviders(<NetworkStrip />, { api: fakeApi({ stats: async () => stats as Stats }) });
    await waitFor(() => expect(item("Height")).toHaveTextContent("Height 25,120,450"));
    expect(strip()).toHaveTextContent("Mainnet");
    expect(item("BZE")).toHaveTextContent("BZE $0.0015▲1.2%");
    expect(item("Block time")).toHaveTextContent("Block time 5.9s");
    expect(item("Txs 24h")).toHaveTextContent("Txs 24h 1,840");
    expect(item("Staked")).toHaveTextContent("Staked 61.2%");
    expect(item("Reward rate")).toHaveTextContent("Reward rate ≈8.9%");
    expect(item("Validators")).toHaveTextContent("Validators 3/40");
  });

  it("marks a falling price down", async () => {
    renderWithProviders(<NetworkStrip />, {
      api: fakeApi({ stats: async () => ({ ...(stats as Stats), price_change_24h_pct: "-2.04" }) }),
    });
    await waitFor(() => expect(item("BZE")).toHaveTextContent("▼2.0%"));
  });

  it("shows a dash for every missing figure", async () => {
    const empty: Stats = {
      ...(stats as Stats),
      latest_height: null,
      avg_block_time_ms: null,
      txs_24h: null,
      staked_share: null,
      reward_rate: null,
      validators: null,
      price_usd: null,
      price_change_24h_pct: null,
    };
    renderWithProviders(<NetworkStrip />, { api: fakeApi({ stats: async () => empty }) });
    await waitFor(() => expect(item("Height")).toHaveTextContent("Height —"));
    for (const label of ["BZE", "Block time", "Txs 24h", "Staked", "Reward rate", "Validators"]) {
      expect(item(label)).toHaveTextContent(`${label} —`);
    }
  });

  it("shows dashes while /stats loads", () => {
    renderWithProviders(<NetworkStrip />, { api: pendingApi() });
    expect(item("Height")).toHaveTextContent("Height —");
  });
});
