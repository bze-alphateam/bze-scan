import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ApiError, type ExplorerApi } from "@/api/client";
import type { DaoList, DaoListParams } from "@/api/types";
import { queryKeys } from "@/api/hooks";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import list from "../../../test/fixtures/api/v1/daos.json";

import { DaosPage } from "./daos-page";

const all = list as DaoList;

function api(extra: Partial<ExplorerApi> = {}): ExplorerApi {
  return fakeApi({
    daos: (p: DaoListParams) => Promise.resolve({ ...all, items: all.items.filter((d) => !p.type || d.type === p.type) }),
    ...extra,
  });
}

const names = () => screen.getAllByTestId("dao-row").map((r) => within(r).getAllByRole("link")[0].textContent);

describe("DaosPage", () => {
  it("shows table rows as skeletons on the first load", () => {
    renderWithProviders(<DaosPage />, { api: pendingApi() });
    expect(screen.getAllByTestId("table-skeleton-row")).toHaveLength(6);
  });

  it("lists the DAOs with their type, admin and age", async () => {
    renderWithProviders(<DaosPage />, { api: api() });
    await screen.findAllByTestId("dao-row");
    expect(names()).toEqual(["Hive Stakers", "Honey Council"]);
    const [stake, council] = screen.getAllByTestId("dao-row");
    expect(within(stake).getAllByTestId("dao-type")[0]).toHaveTextContent("Stakers");
    expect(within(stake).getByRole("link", { name: "Hive Stakers" })).toHaveAttribute("href", "/daos/2");
    expect(stake).toHaveTextContent("#2");
    // A DAO run by another DAO shows it by its label, one run by an account the short address.
    expect(within(stake).getByTestId("dao-admin")).toHaveTextContent("DAO #1 · Honey Council");
    expect(within(council).getByTestId("dao-admin")).toHaveTextContent("bze1a56…j0vg");
    expect(within(council).getAllByTestId("dao-type")[0]).toHaveTextContent("Member list");
  });

  it("says Self-governed for a DAO that owns itself", async () => {
    const items = all.items.map((d, i) => (i === 1 ? { ...d, admin: d.address, self_governed: true } : d));
    renderWithProviders(<DaosPage />, { api: api({ daos: () => Promise.resolve({ ...all, items }) }) });
    await screen.findAllByTestId("dao-row");
    expect(within(screen.getAllByTestId("dao-row")[1]).getByTestId("dao-admin")).toHaveTextContent("Self-governed");
  });

  it("filters by type through the API", async () => {
    const daos = vi.fn(api().daos);
    renderWithProviders(<DaosPage />, { api: api({ daos }) });
    await screen.findAllByTestId("dao-row");
    await userEvent.click(screen.getByRole("button", { name: "Stakers" }));
    await screen.findByText("Hive Stakers");
    expect(daos).toHaveBeenLastCalledWith(expect.objectContaining({ type: "stake" }), expect.anything());
    expect(screen.getAllByTestId("dao-row")).toHaveLength(1);
  });

  it("shows the open proposals badge only when the API sends it", async () => {
    const items = all.items.map((d, i) => (i === 0 ? { ...d, proposals_open: 2 } : d));
    renderWithProviders(<DaosPage />, { api: api({ daos: () => Promise.resolve({ ...all, items }) }) });
    await screen.findAllByTestId("dao-row");
    expect(screen.getAllByTestId("proposals-open")).toHaveLength(1);
    expect(screen.getByTestId("proposals-open")).toHaveTextContent("2 open proposals");
  });

  it("follows the cursor with Show more", async () => {
    const daos = vi.fn((p: DaoListParams) =>
      Promise.resolve(p.cursor ? { items: [all.items[1]], next_cursor: null, supported: true } : { items: [all.items[0]], next_cursor: "c1", supported: true }),
    );
    renderWithProviders(<DaosPage />, { api: api({ daos }) });
    await screen.findAllByTestId("dao-row");
    await userEvent.click(screen.getByRole("button", { name: "Show more" }));
    await screen.findByText("Honey Council");
    expect(names()).toEqual(["Hive Stakers", "Honey Council"]);
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("links the DAO app, and says how to create one on an empty list", async () => {
    renderWithProviders(<DaosPage />, { api: api({ daos: () => Promise.resolve({ items: [], next_cursor: null, supported: true }) }) });
    const empty = await screen.findByTestId("daos-empty");
    expect(within(empty).getByRole("link", { name: "Create one in the DAO app" })).toHaveAttribute("href", "https://dao.getbze.com/create");
    expect(within(empty).getByRole("link")).toHaveAttribute("target", "_blank");
    expect(screen.getByTestId("dao-app-link")).toHaveAttribute("href", "https://dao.getbze.com");
  });

  it("polls the first page only: once an older page is loaded the list stops polling", async () => {
    const daos = (p: DaoListParams) =>
      Promise.resolve(p.cursor ? { items: [all.items[1]], next_cursor: null, supported: true } : { items: [all.items[0]], next_cursor: "c1", supported: true });
    const { queryClient } = renderWithProviders(<DaosPage />, { api: api({ daos }) });
    await screen.findAllByTestId("dao-row");
    const query = () => queryClient.getQueryCache().find({ queryKey: queryKeys.daos(null) });
    const interval = () => (query()?.options as { refetchInterval: (q: unknown) => number | false }).refetchInterval(query());
    expect(interval()).toBe(4000);
    await userEvent.click(screen.getByRole("button", { name: "Show more" }));
    await screen.findByText("Honey Council");
    expect(interval()).toBe(false);
  });

  it("says DAOs arrive with v8.3.0 before the upgrade", async () => {
    renderWithProviders(<DaosPage />, { api: api({ daos: () => Promise.resolve({ items: [], next_cursor: null, supported: false }) }) });
    expect(await screen.findByTestId("daos-unsupported")).toHaveTextContent("DAOs arrive with chain v8.3.0");
    expect(screen.queryByRole("group", { name: "Filter by type" })).toBeNull();
  });

  it("shows an error with a retry", async () => {
    const daos = vi.fn().mockRejectedValueOnce(new ApiError(502, "upstream_error", "x")).mockResolvedValue(all);
    renderWithProviders(<DaosPage />, { api: api({ daos }) });
    expect(await screen.findByTestId("daos-error")).toBeInTheDocument();
  });
});
