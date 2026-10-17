import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { ChainParams, HallOfFame, ValidatorDetail, ValidatorList } from "@/api/types";
import { websiteLink } from "@/lib/links";
import { appIconsIn, fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import params from "../../../test/fixtures/api/v1/params.json";
import fame from "../../../test/fixtures/api/v1/validators/hall-of-fame.json";
import detail from "../../../test/fixtures/api/v1/validators/bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c.json";
import list from "../../../test/fixtures/api/v1/validators.json";

import { ValidatorPage } from "./validator-page";

const OP = detail.operator_address;

function api(v: unknown = detail, extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    validator: () => Promise.resolve(v as ValidatorDetail),
    validators: () => Promise.resolve(list as ValidatorList),
    params: () => Promise.resolve(params as ChainParams),
    hallOfFame: () => Promise.resolve(fame as HallOfFame),
    ...extra,
  });
}

describe("ValidatorPage", () => {
  it("shows the header, tile and card skeletons on the first load", () => {
    renderWithProviders(<ValidatorPage operator={OP} />, { api: pendingApi() });
    expect(screen.getByTestId("validator-skeleton")).toBeInTheDocument();
    expect(screen.getAllByTestId("tile-skeleton")).toHaveLength(4);
    expect(screen.getAllByTestId("card-skeleton")).toHaveLength(3);
  });

  it("renders the header, the tiles, about, recent blocks and history", async () => {
    renderWithProviders(<ValidatorPage operator={OP} />, { api: api() });

    expect(await screen.findByRole("heading", { level: 1, name: "Honeycomb" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Validators/Honeycomb");
    expect(await screen.findByText("Active · rank 1 of 3")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /honeycomb\.example\.org/ })).toHaveAttribute("href", "https://honeycomb.example.org/");
    expect(screen.getByText("Validating since Apr 17, 2022 · 1,204 accounts stake here")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Stake with this validator/ })).toHaveAttribute(
      "href",
      "https://staking.getbze.com/?stake=bzevaloper1hc9t7nwlq3n9y4n4ld0r0z5sfg3l5egwqk5w6c",
    );
    expect(appIconsIn(screen.getByRole("link", { name: /Stake with this validator/ }))).toEqual(["staking"]);

    const tiles = screen.getAllByTestId("tile").map((t) => t.textContent);
    expect(tiles).toEqual([
      "Staked with them50M BZE12.3% of all staked BZE",
      "Commission5%can change by at most 5 points a day, never above 30%",
      "Your reward rate here≈ 8.5%8.9% base, minus the 5% commission",
      "Uptime99.9%missed 1 of the last 10,000 blocks",
    ]);

    const about = screen.getByRole("heading", { name: "About" }).closest("section")!;
    expect(within(about).getByText(OP)).toBeInTheDocument();
    expect(within(about).getByRole("link", { name: "bze18uf…9zhw" })).toHaveAttribute(
      "href",
      "/accounts/bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw",
    );
    expect(about).toHaveTextContent("120,000 BZE · the validator's own money at risk");
    expect(about).toHaveTextContent("ops@honeycomb.example.org");

    const blocks = screen.getAllByTestId("recent-block");
    expect(blocks).toHaveLength(3);
    expect(within(blocks[0]).getByRole("link", { name: "Block 25,120,440" })).toHaveAttribute("href", "/blocks/25120440");
    expect(blocks[0]).toHaveTextContent("7 transactions");
    expect(blocks[1]).toHaveTextContent("1 transaction");
    expect(blocks[2]).toHaveTextContent("no transactions");
    expect(screen.getByText("1 in every 8 blocks, on average")).toBeInTheDocument();

    const history = screen.getAllByTestId("history-item");
    expect(history.map((h) => h.firstChild?.textContent)).toEqual([
      "Commission changed from 4% to 5%",
      "Joined the active set",
      "Created with 4% commission",
    ]);
    expect(within(history[0]).getByRole("link", { name: "see transaction" })).toHaveAttribute(
      "href",
      expect.stringMatching(/^\/txs\/9C1E/),
    );
    expect(within(history[1]).queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText("Never jailed, never penalised since Apr 17, 2022")).toBeInTheDocument();

    const votes = screen.getByTestId("validator-votes");
    const rows = within(votes).getAllByTestId("validator-vote");
    expect(rows.map((r) => r.textContent)).toEqual(["Yes#14 Raise the block size", "Split vote#12 Community call"]);
    expect(within(rows[0]).getByRole("link", { name: "#14 Raise the block size" })).toHaveAttribute("href", "/proposals/14");
    expect(within(votes).getByRole("link", { name: "#14 Raise the block size" })).toHaveAttribute("href", "/proposals/14");
  });

  it("keeps the details collapsed until asked, then shows them with the raw JSON", async () => {
    renderWithProviders(<ValidatorPage operator={OP} />, { api: api() });
    const more = await screen.findByRole("button", { name: /More details/ });
    expect(screen.queryByText("Consensus key")).not.toBeInTheDocument();
    await userEvent.click(more);
    expect(screen.getByText("RQzL8AijestH86OmhuuzMn/L7HNNARaDoONs2H+Wo+c=")).toBeInTheDocument();
    expect(screen.getByText("Commission limits").nextElementSibling).toHaveTextContent("max 30% · max change 5 points per day");
    expect(screen.getByText("1 ubze")).toBeInTheDocument();
    expect(screen.getByText("50,000,000")).toBeInTheDocument();
    expect(screen.getByText(/"delegator_count": 1204/)).toBeInTheDocument();
    // On the raw bar from c2 up, under the JSON on phones.
    for (const link of screen.getAllByRole("link", { name: /Open on Ping.pub/ })) {
      expect(link).toHaveAttribute("href", `https://explorer.getbze.com/beezee/staking/${OP}`);
    }
  });

  it("marks a Hall of Fame member gold with the crown card, linking to the Hall of Fame", async () => {
    renderWithProviders(<ValidatorPage operator={OP} />, { api: api() });
    const crown = await screen.findByTestId("fame-crown");
    const title = screen.getByRole("heading", { level: 1, name: "Honeycomb" });
    expect(title.querySelector("[data-gold]")).not.toBeNull();
    expect(document.querySelector('[data-ring="gold"]')).not.toBeNull();
    await userEvent.hover(within(crown).getByRole("button", { name: "Hall of Fame member" }));
    const card = within(crown).getByRole("tooltip");
    expect(card).toHaveTextContent("Missed 1 of the last 10,000 blocks (10 allowed)");
    expect(within(card).getByRole("link", { name: "See the Hall of Fame →" })).toHaveAttribute("href", "/validators");
  });

  it("marks nobody who is not in the Hall of Fame, nor while it fails", async () => {
    const outside = { ...fame, items: fame.items.filter((v) => v.operator_address !== OP) };
    const { unmount } = renderWithProviders(<ValidatorPage operator={OP} />, {
      api: api(detail, { hallOfFame: () => Promise.resolve(outside as HallOfFame) }),
    });
    expect(await screen.findByText("Active · rank 1 of 3")).toBeInTheDocument();
    expect(screen.queryByTestId("fame-crown")).not.toBeInTheDocument();
    expect(document.querySelector('[data-ring="gold"], [data-gold]')).toBeNull();
    unmount();

    renderWithProviders(<ValidatorPage operator={OP} />, { api: api(detail, { hallOfFame: () => Promise.reject(new Error("down")) }) });
    expect(await screen.findByText("Active · rank 1 of 3")).toBeInTheDocument();
    expect(screen.queryByTestId("fame-crown")).not.toBeInTheDocument();
  });

  it("names a jailed validator and its penalties, and one that never voted", async () => {
    const jailed = {
      ...detail,
      jailed: true,
      status: "unbonding",
      rank: null,
      voting_power_pct: null,
      jailed_until: "2099-01-01T00:00:00Z",
      events: [
        { height: 30, tx_index: -1, tx_hash: null, kind: "jailed", details: { from: false, to: true }, time: "2026-10-01T00:00:00Z" },
        {
          height: 30,
          tx_index: -1,
          tx_hash: null,
          kind: "slashed",
          details: { reason: "missing_signature" },
          time: "2026-10-01T00:00:00Z",
        },
      ],
      votes: [],
    };
    renderWithProviders(<ValidatorPage operator={OP} />, { api: api(jailed) });
    expect(await screen.findByTestId("validator-status")).toHaveTextContent("Jailed until Jan 1, 2099, 00:00:00 UTC");
    expect(screen.getByText("Penalised for missing too many blocks")).toBeInTheDocument();
    expect(screen.queryByText(/Never jailed/)).not.toBeInTheDocument();
    expect(screen.getByText("Honeycomb has not voted yet.")).toBeInTheDocument();
    expect(screen.getAllByTestId("tile")[0]).toHaveTextContent("outside the active set");
  });

  it("tolerates a null summary in the reward tile", async () => {
    renderWithProviders(<ValidatorPage operator={OP} />, {
      api: api(detail, { validators: () => Promise.resolve({ ...list, summary: null } as ValidatorList) }),
    });
    await screen.findByText("Active · rank 1 of 3");
    expect(screen.getAllByTestId("tile")[2]).toHaveTextContent("Your reward rate here—the base rate minus the commission");
  });

  it("says when the validator is unknown", async () => {
    const validator = vi.fn(() => Promise.reject(new ApiError(404, "not_found", "validator not synced")));
    renderWithProviders(<ValidatorPage operator={OP} />, { api: api(detail, { validator }) });
    expect(await screen.findByRole("heading", { name: "Validator not found" })).toBeInTheDocument();
    expect(screen.getByTestId("validator-not_found")).toHaveAttribute("data-state", "neutral");
    expect(screen.getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Validators/bzevaloper1hc9…5w6c");
    expect(screen.getByRole("link", { name: "Look it up on Ping.pub ↗" })).toHaveAttribute("href", expect.stringContaining(`/staking/${OP}`));
  });

  it("offers a retry when the API fails", async () => {
    const validator = vi.fn(() => Promise.reject(new ApiError(500, "internal", "boom")));
    renderWithProviders(<ValidatorPage operator={OP} />, { api: api(detail, { validator }) });
    expect(await screen.findByTestId("validator-error")).toHaveAttribute("data-state", "error");
    expect(screen.getByRole("heading", { name: "This validator could not be loaded" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(validator).toHaveBeenCalledTimes(2);
  });
});

describe("websiteLink", () => {
  it("shows the host and path, and adds the scheme a validator left out", () => {
    expect(websiteLink("https://www.linktr.ee/jasonsopko/")).toEqual({
      href: "https://www.linktr.ee/jasonsopko/",
      text: "linktr.ee/jasonsopko",
    });
    expect(websiteLink("getbze.com")).toEqual({ href: "https://getbze.com/", text: "getbze.com" });
  });

  it("refuses anything that is not a web address", () => {
    expect(websiteLink("javascript:alert(1)")).toBeNull();
    expect(websiteLink(null)).toBeNull();
    expect(websiteLink("")).toBeNull();
  });
});
