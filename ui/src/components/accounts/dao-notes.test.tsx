import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Account, DaoList } from "@/api/types";
import { fakeApi, renderWithProviders } from "@/test/render";

import daosList from "../../../test/fixtures/api/v1/daos.json";
import account from "../../../test/fixtures/api/v1/accounts/bze18mhtjwczlzqqgvw84uz8lrdv4hqule3jp8allp.json";

import { AdminOfDaos, DaoBanner } from "./dao-notes";

const daoAccount = { ...account, address: daosList.items[1].address, label: { name: "DAO #1 · Honey Council", kind: "dao" } } as Account;

describe("DaoBanner", () => {
  it("says whose treasury an address is and links the DAO page", () => {
    renderWithProviders(<DaoBanner a={daoAccount} />);
    const banner = screen.getByTestId("dao-banner");
    expect(banner).toHaveTextContent("This is the treasury of DAO #1 · Honey Council");
    expect(within(banner).getByRole("link", { name: "Open the DAO page →" })).toHaveAttribute("href", "/daos/1");
  });

  it("shows nothing on any other account", () => {
    renderWithProviders(<DaoBanner a={account as Account} />);
    expect(screen.queryByTestId("dao-banner")).toBeNull();
  });
});

describe("AdminOfDaos", () => {
  const admin = daosList.items[1].admin;

  it("lists the DAOs the address runs", async () => {
    renderWithProviders(<AdminOfDaos address={admin} />, { api: fakeApi({ daos: () => Promise.resolve(daosList as DaoList) }) });
    const note = await screen.findByTestId("admin-of-daos");
    expect(note).toHaveTextContent("Admin of 1 DAO:DAO #1 · Honey Council");
    expect(within(note).getByRole("link")).toHaveAttribute("href", "/daos/1");
  });

  it("follows the cursor, so the count is right above one page", async () => {
    const mine = (id: number) => ({ ...daosList.items[1], id, name: `D${id}` });
    const daos = (p: { cursor?: string }) =>
      Promise.resolve(
        p.cursor
          ? { items: [mine(3)], next_cursor: null, supported: true }
          : { items: [mine(1), mine(2)], next_cursor: "c1", supported: true },
      );
    renderWithProviders(<AdminOfDaos address={admin} />, { api: fakeApi({ daos }) });
    expect(await screen.findByTestId("admin-of-daos")).toHaveTextContent("Admin of 3 DAOs:");
  });

  it("shows nothing when the address runs none", async () => {
    renderWithProviders(<AdminOfDaos address="bze1nobody" />, { api: fakeApi({ daos: () => Promise.resolve(daosList as DaoList) }) });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByTestId("admin-of-daos")).toBeNull();
  });

  it("shows nothing while loading or when the API fails", async () => {
    renderWithProviders(<AdminOfDaos address={admin} />, { api: fakeApi({ daos: () => Promise.reject(new Error("down")) }) });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByTestId("admin-of-daos")).toBeNull();
  });
});
