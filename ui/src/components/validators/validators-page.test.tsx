import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { ExplorerApi } from "@/api/client";
import type { ChainParams, HallOfFame, ValidatorList } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import params from "../../../test/fixtures/api/v1/params.json";
import fame from "../../../test/fixtures/api/v1/validators/hall-of-fame.json";
import list from "../../../test/fixtures/api/v1/validators.json";

import { SummaryTiles, ValidatorsPage } from "./validators-page";

function api(extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    validators: () => Promise.resolve(list as ValidatorList),
    hallOfFame: () => Promise.resolve(fame as HallOfFame),
    params: () => Promise.resolve(params as ChainParams),
    ...extra,
  });
}

/** The Hall of Fame opens first: switch to the Active tab and wait for its rows. */
async function openActive() {
  await userEvent.click(await screen.findByRole("tab", { name: "Active 3" }));
  await waitFor(() => expect(screen.getAllByTestId("validator-row")).toHaveLength(3));
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

    const tabs = screen.getAllByRole("tab").map((t) => t.textContent);
    expect(tabs).toEqual(["Hall of Fame 2", "Active 3", "Inactive 2"]);
    expect(screen.getByRole("tab", { name: "Hall of Fame 2" })).toHaveAttribute("aria-selected", "true");
    await openActive();
    expect(monikers()).toEqual(["Honeycomb", "Thamar Node", "Deep Fields"]);
    // The Hall of Fame members are marked in the active list too: a gold ring, a gold name and a crown.
    const marks = screen.getAllByTestId("validator-row").map((r) => ({
      ring: r.querySelector('[data-ring="gold"]') !== null,
      gold: r.querySelector("[data-gold]") !== null,
      crown: within(r).queryByTestId("fame-crown") !== null,
    }));
    expect(marks).toEqual([
      { ring: true, gold: true, crown: true },
      { ring: true, gold: true, crown: true },
      { ring: false, gold: false, crown: false },
    ]);

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
      "https://staking.getbze.com/?stake=bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c",
    );

    expect(await screen.findByText(/share of the last 10,000 blocks the validator signed/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Picking a validator" })).toBeInTheDocument();
  });

  it("switches to the inactive tab: no rank, a JAILED tag on jailed rows, unbonding named, no stake", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api() });
    await userEvent.click(await screen.findByRole("tab", { name: "Inactive 2" }));
    await waitFor(() => expect(monikers()).toEqual(["Sleepy Node", "Old Timer"]));
    const [sleepy, old] = screen.getAllByTestId("validator-row");
    expect(sleepy.firstChild).toHaveTextContent("–");
    expect(within(sleepy).getByText("Jailed")).toHaveAttribute("data-tag", "bad");
    expect(within(sleepy).getByText("Unbonding")).toBeInTheDocument();
    expect(within(old).queryByText("Jailed")).not.toBeInTheDocument();
    expect(old).toHaveTextContent("—");
    // Inactive validators pay no rewards: no Stake button on their rows.
    expect(within(sleepy).queryByRole("link", { name: /^Stake with/ })).not.toBeInTheDocument();
    expect(within(old).queryByRole("link", { name: /^Stake with/ })).not.toBeInTheDocument();
  });

  it("lists the Hall of Fame, the smallest first, with what earned each its place", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api() });
    expect(await screen.findByTestId("hall-of-fame-intro")).toHaveTextContent(
      "voted on every one of the last 10 proposals, whatever their vote, and missed no more than 10 of the last 10,000 blocks",
    );
    const rows = screen.getAllByTestId("hall-of-fame-row");
    // Deep Fields missed 160 blocks, Sleepy Node is not active and voted on 4: neither is listed.
    expect(rows.map((r) => within(r).getAllByRole("link")[0].textContent)).toEqual(["Thamar Node", "Honeycomb"]);
    expect(within(rows[0]).getByTestId("hall-of-fame-signing")).toHaveTextContent("Didn't miss a block in the last 10,000 blocks");
    expect(within(rows[1]).getByTestId("hall-of-fame-signing")).toHaveTextContent("Missed 1 of the last 10,000 blocks");
    // Within the rule's allowance: the ok tone. Every member is gold.
    expect(within(rows[1]).getByTestId("hall-of-fame-signing")).toHaveAttribute("data-chip", "ok");
    expect(rows.every((r) => r.querySelector('[data-ring="gold"]') && r.querySelector("[data-gold]"))).toBe(true);
    expect(within(rows[1]).getByTestId("hall-of-fame-votes")).toHaveTextContent("Voted on all of the last 10 proposals");
    expect(rows[1]).toHaveTextContent("50M BZE · 12.3% of the voting power");

    // The list ends with a way to every active validator.
    await userEvent.click(screen.getByRole("button", { name: "See all active validators" }));
    expect(screen.getByRole("tab", { name: "Active 3" })).toHaveAttribute("aria-selected", "true");
    await waitFor(() => expect(monikers()).toEqual(["Honeycomb", "Thamar Node", "Deep Fields"]));

    await userEvent.click(screen.getByRole("tab", { name: "Hall of Fame 2" }));
    await userEvent.type(screen.getByRole("textbox", { name: "Find a validator" }), "zzz");
    expect(await screen.findByText("No Hall of Fame validator has that name.")).toBeInTheDocument();
  });

  it("words the Hall of Fame with the rule the API states", async () => {
    const rules = { proposals: 10, max_missed_blocks: 5, signed_blocks_window: 20_000 };
    renderWithProviders(<ValidatorsPage />, { api: api({ hallOfFame: () => Promise.resolve({ ...fame, rules } as HallOfFame) }) });
    expect(await screen.findByTestId("hall-of-fame-intro")).toHaveTextContent(
      "voted on every one of the last 10 proposals, whatever their vote, and missed no more than 5 of the last 20,000 blocks",
    );
    await openActive();
    const crown = within(screen.getAllByTestId("validator-row")[0]).getByTestId("fame-crown");
    await userEvent.hover(within(crown).getByRole("button", { name: "Hall of Fame member" }));
    expect(within(crown).getByRole("tooltip")).toHaveTextContent("Missed 1 of the last 20,000 blocks (5 allowed)");
  });

  it("counts the closed proposals while fewer have closed than the rule asks", async () => {
    const few = { ...fame, items: fame.items.map((v) => ({ ...v, recent_votes: { voted: 3, of: 3 } })) };
    renderWithProviders(<ValidatorsPage />, { api: api({ hallOfFame: () => Promise.resolve(few as HallOfFame) }) });
    expect(await screen.findByTestId("hall-of-fame-intro")).toHaveTextContent("voted on every one of the last 3 proposals");
  });

  it("hides an empty Hall of Fame and opens on Active", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api({ hallOfFame: () => Promise.resolve({ ...fame, items: [] } as HallOfFame) }) });
    expect(await screen.findByRole("tab", { name: "Active 3" })).toHaveAttribute("aria-selected", "true");
    await waitFor(() => expect(screen.getAllByTestId("validator-row")).toHaveLength(3));
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Active 3", "Inactive 2"]);
    expect(screen.queryByText("No validator meets the bar right now.")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "See all active validators" })).not.toBeInTheDocument();
    expect(screen.queryByTestId("fame-crown")).not.toBeInTheDocument();
  });

  it("opens on the Hall of Fame when it has members", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api() });
    expect(await screen.findByRole("tab", { name: "Hall of Fame 2" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Active 3" })).toHaveAttribute("aria-selected", "false");
    expect(screen.getAllByTestId("hall-of-fame-row")).toHaveLength(2);
  });

  it("shows Active and marks nobody while the Hall of Fame loads, then switches to it once members arrive", async () => {
    let answer: (f: HallOfFame) => void = () => {};
    const pending = new Promise<HallOfFame>((resolve) => (answer = resolve));
    renderWithProviders(<ValidatorsPage />, { api: api({ hallOfFame: () => pending }) });
    expect(await screen.findByRole("tab", { name: "Active 3" })).toHaveAttribute("aria-selected", "true");
    await waitFor(() => expect(screen.getAllByTestId("validator-row")).toHaveLength(3));
    expect(screen.queryByRole("tab", { name: /Hall of Fame/ })).not.toBeInTheDocument();
    expect(screen.queryByTestId("fame-crown")).not.toBeInTheDocument();
    expect(document.querySelector('[data-ring="gold"], [data-gold]')).toBeNull();

    await act(async () => answer(fame as HallOfFame));
    expect(await screen.findByRole("tab", { name: "Hall of Fame 2" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getAllByTestId("hall-of-fame-row")).toHaveLength(2);
  });

  it("stays on the tab the visitor picked when the Hall of Fame arrives later", async () => {
    let answer: (f: HallOfFame) => void = () => {};
    const pending = new Promise<HallOfFame>((resolve) => (answer = resolve));
    renderWithProviders(<ValidatorsPage />, { api: api({ hallOfFame: () => pending }) });
    await userEvent.click(await screen.findByRole("tab", { name: "Inactive 2" }));
    await waitFor(() => expect(monikers()).toEqual(["Sleepy Node", "Old Timer"]));

    await act(async () => answer(fame as HallOfFame));
    expect(await screen.findByRole("tab", { name: "Hall of Fame 2" })).toHaveAttribute("aria-selected", "false");
    expect(screen.getByRole("tab", { name: "Inactive 2" })).toHaveAttribute("aria-selected", "true");
    expect(monikers()).toEqual(["Sleepy Node", "Old Timer"]);
  });

  it("hides the Hall of Fame when it fails, opens on Active and marks nobody", async () => {
    const hallOfFame = vi.fn(() => Promise.reject(new Error("boom")));
    renderWithProviders(<ValidatorsPage />, { api: api({ hallOfFame }) });
    expect(await screen.findByRole("tab", { name: "Active 3" })).toHaveAttribute("aria-selected", "true");
    await waitFor(() => expect(hallOfFame).toHaveBeenCalled());
    await waitFor(() => expect(screen.getAllByTestId("validator-row")).toHaveLength(3));
    expect(screen.queryByRole("tab", { name: /Hall of Fame/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "The Hall of Fame could not be loaded" })).not.toBeInTheDocument();
    expect(screen.queryByTestId("fame-crown")).not.toBeInTheDocument();
    expect(document.querySelector('[data-ring="gold"], [data-gold]')).toBeNull();
  });

  it("opens the crown's card on hover with the validator's own figures and the rule's thresholds", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api() });
    await openActive();
    const crown = within(screen.getAllByTestId("validator-row")[0]).getByTestId("fame-crown");
    // Closed, the card is hidden from the accessibility tree too.
    const card = within(crown).getByRole("tooltip", { hidden: true });
    expect(card).not.toBeVisible();
    const button = within(crown).getByRole("button", { name: "Hall of Fame member" });
    expect(button).toHaveAttribute("aria-describedby", card.id);

    await userEvent.hover(button);
    expect(card).toBeVisible();
    // Honeycomb: all 10 proposals voted on, 1 block missed of the rule's 10,000 window, 10 allowed.
    expect(card).toHaveTextContent("Hall of Fame");
    expect(card).toHaveTextContent("Voted on all of the last 10 proposals");
    expect(card).toHaveTextContent("Missed 1 of the last 10,000 blocks (10 allowed)");
    await userEvent.unhover(button);
    expect(card).not.toBeVisible();
  });

  it("opens the crown's card on keyboard focus, and its link switches to the Hall of Fame tab", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api() });
    await openActive();
    const row = screen.getAllByTestId("validator-row")[1];
    act(() => within(row).getByRole("link", { name: "Thamar Node" }).focus());
    await userEvent.tab();
    const button = within(row).getByRole("button", { name: "Hall of Fame member" });
    expect(button).toHaveFocus();
    const card = within(row).getByRole("tooltip");
    expect(card).toBeVisible();
    expect(card).toHaveTextContent("Missed no blocks in the last 10,000 (10 allowed)");

    // Escape closes it; focus brings it back, and the link inside takes the tab key.
    await userEvent.keyboard("{Escape}");
    expect(card).not.toBeVisible();
    await userEvent.tab({ shift: true });
    await userEvent.tab();
    expect(card).toBeVisible();
    await userEvent.tab();
    const see = within(card).getByRole("button", { name: "See the Hall of Fame →" });
    expect(see).toHaveFocus();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByRole("tab", { name: "Hall of Fame 2" })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findAllByTestId("hall-of-fame-row")).toHaveLength(2);
  });

  it("opens the crown's card on a tap and closes it on a tap elsewhere", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api() });
    await openActive();
    const crown = within(screen.getAllByTestId("validator-row")[0]).getByTestId("fame-crown");
    const card = within(crown).getByRole("tooltip", { hidden: true });
    fireEvent.click(within(crown).getByRole("button", { name: "Hall of Fame member" }));
    expect(card).toBeVisible();
    fireEvent.pointerDown(document.body);
    expect(card).not.toBeVisible();
  });

  it("shows each validator's picture, else its initials", async () => {
    const withLogo = { ...list, items: list.items.map((v, i) => (i === 0 ? { ...v, logo_url: "https://pictures.example/v.jpg" } : v)) };
    renderWithProviders(<ValidatorsPage />, { api: api({ validators: () => Promise.resolve(withLogo as ValidatorList) }) });
    await openActive();
    const [first, second] = screen.getAllByTestId("validator-row");
    expect(within(first).getByTestId("avatar-image")).toHaveAttribute("src", "https://pictures.example/v.jpg");
    expect(within(second).queryByTestId("avatar-image")).not.toBeInTheDocument();
  });

  it("narrows the rows by moniker", async () => {
    renderWithProviders(<ValidatorsPage />, { api: api() });
    await openActive();
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
    expect(await screen.findByRole("tab", { name: "Active 3" })).toBeInTheDocument();
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
    expect(await screen.findByRole("heading", { name: "The validators could not be loaded" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
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
