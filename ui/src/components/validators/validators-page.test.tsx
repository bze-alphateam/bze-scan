import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { ExplorerApi } from "@/api/client";
import type { ChainParams, ValidatorList } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import params from "../../../test/fixtures/api/v1/params.json";
import list from "../../../test/fixtures/api/v1/validators.json";

import { SummaryTiles, ValidatorsPage } from "./validators-page";

function api(extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    validators: () => Promise.resolve(list as ValidatorList),
    params: () => Promise.resolve(params as ChainParams),
    ...extra,
  });
}

const monikers = () =>
  screen.getAllByTestId("validator-row").map((r) => within(r).getAllByRole("link")[0].textContent);

describe("ValidatorsPage", () => {
  it("shows four tile skeletons and ten row skeletons on the first load", () => {
    renderWithProviders(<ValidatorsPage />, { api: pendingApi() });
    expect(screen.getByTestId("validators-skeleton")).toBeInTheDocument();
    expect(screen.getAllByTestId("tile-skeleton")).toHaveLength(4);
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(10);
  });

  it("renders the intro, the tiles and the active validators", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api() });
    expect(await screen.findByText(/3 validators produce blocks right now, out of 40 seats/)).toBeInTheDocument();

    const tiles = screen.getAllByTestId("tile").map((t) => t.textContent);
    expect(tiles).toEqual([
      "Staked BZE405M BZE61.2% of all BZE is staked",
      "Reward rate≈ 8.9%per year before the validator's commission",
      "Unstaking takes21 daysno rewards while unstaking · see parameters",
      "Top 5 validators hold40.2%a healthier network keeps this low",
    ]);

    expect(screen.getByRole("tab", { name: "Active · 3" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Inactive · 2" })).toBeInTheDocument();
    expect(monikers()).toEqual(["Honeycomb", "Thamar Node", "Deep Fields"]);

    const honeycomb = screen.getAllByTestId("validator-row")[0];
    expect(within(honeycomb).getByRole("link", { name: "Honeycomb" })).toHaveAttribute(
      "href",
      "/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c",
    );
    expect(honeycomb).toHaveTextContent("50M BZE · 12.3%");
    expect(honeycomb).toHaveTextContent("5%");
    expect(honeycomb).toHaveTextContent("99.9%");
    expect(within(honeycomb).getAllByRole("link", { name: "Stake with Honeycomb" })[0]).toHaveAttribute(
      "href",
      "https://staking.getbze.com",
    );

    expect(await screen.findByText(/share of the last 10,000 blocks the validator signed/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Picking a validator" })).toBeInTheDocument();
  });

  it("switches to the inactive tab, which shows the status instead of a rank", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api() });
    await userEvent.click(await screen.findByRole("tab", { name: "Inactive · 2" }));
    // The Active panel unmounts once its exit animation ends, and until then it keeps its own rows.
    await waitFor(() => expect(monikers()).toEqual(["Sleepy Node", "Old Timer"]));
    const [sleepy, old] = screen.getAllByTestId("validator-row");
    expect(sleepy).toHaveTextContent("Unbonding");
    expect(within(sleepy).getByText("Jailed")).toBeInTheDocument();
    expect(old).toHaveTextContent("Not active");
    expect(old).toHaveTextContent("—");
  });

  it("shows each validator's picture, else its initials", async () => {
    const withLogo = { ...list, items: list.items.map((v, i) => (i === 0 ? { ...v, logo_url: "https://pictures.example/v.jpg" } : v)) };
    renderWithProviders(<ValidatorsPage />, { api: api({ validators: () => Promise.resolve(withLogo as ValidatorList) }) });
    await screen.findAllByTestId("validator-row");
    const [first, second] = screen.getAllByTestId("validator-row");
    expect(within(first).getByTestId("avatar-image")).toHaveAttribute("src", "https://pictures.example/v.jpg");
    expect(within(second).queryByTestId("avatar-image")).not.toBeInTheDocument();
  });

  it("narrows the rows by moniker", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api() });
    const input = await screen.findByRole("textbox", { name: "Find a validator" });
    await userEvent.type(input, "thamar");
    expect(monikers()).toEqual(["Thamar Node"]);
    await userEvent.clear(input);
    await userEvent.type(input, "nobody");
    expect(screen.queryAllByTestId("validator-row")).toHaveLength(0);
    expect(screen.getByText("No active validator has that name.")).toBeInTheDocument();
  });

  it("follows next_cursor to the last page", async () => {
    const [first, ...rest] = list.items;
    const validators = vi.fn((p: { cursor?: string }) =>
      Promise.resolve(
        (p.cursor
          ? { items: rest, next_cursor: null, summary: null }
          : { items: [first], next_cursor: "c1", summary: list.summary }) as ValidatorList,
      ),
    );
    renderWithProviders(<ValidatorsPage />, { api: api({ validators }) });
    expect(await screen.findByRole("tab", { name: "Active · 3" })).toBeInTheDocument();
    expect(validators).toHaveBeenCalledTimes(2);
    expect(validators.mock.calls[1][0]).toEqual({ status: "all", limit: 100, cursor: "c1" });
  });

  it("falls back to the usual window when the parameters cannot be read", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api({ params: () => Promise.reject(new Error("down")) }) });
    expect(await screen.findByText(/share of the last 10,000 blocks/)).toBeInTheDocument();
  });

  it("offers a retry when the API fails", async () => {
    const validators = vi.fn(() => Promise.reject(new Error("boom")));
    renderWithProviders(<ValidatorsPage />, { api: api({ validators }) });
    await userEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(validators).toHaveBeenCalledTimes(2);
  });
});

describe("SummaryTiles", () => {
  it("shows a dash in every tile of a null summary", () => {
    renderWithProviders(<SummaryTiles summary={null} />);
    const values = screen.getAllByTestId("tile").map((t) => t.children[1].textContent);
    expect(values).toEqual(["—", "—", "—", "—"]);
  });

  it("shows a dash for each null field", () => {
    renderWithProviders(
      <SummaryTiles
        summary={{
          bonded_tokens: null,
          staked_share: null,
          reward_rate: "8.9",
          unbonding_period: null,
          top5_share: null,
          max_validators: null,
        }}
      />,
    );
    const values = screen.getAllByTestId("tile").map((t) => t.children[1].textContent);
    expect(values).toEqual(["—", "≈ 8.9%", "—", "—"]);
  });
});
