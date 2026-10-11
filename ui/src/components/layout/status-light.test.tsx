import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Status } from "@/api/types";
import { fakeApi, pendingApi, renderWithProviders } from "@/test/render";

import healthy from "../../../test/fixtures/api/v1/status.json";
import behind from "../../../test/fixtures/variants/status-behind.json";

import { StatusLight } from "./status-light";

const trigger = () => screen.getByRole("button", { name: /^Explorer status/ });

describe("StatusLight", () => {
  it("is grey before the first answer", () => {
    renderWithProviders(<StatusLight />, { api: pendingApi() });
    expect(trigger()).toHaveAttribute("data-light", "unknown");
    expect(trigger()).toHaveTextContent("Unknown");
  });

  it("is green when live indexing keeps up, with the heights in its popover", async () => {
    renderWithProviders(<StatusLight />, { api: fakeApi({ status: async () => healthy as Status }) });
    await waitFor(() => expect(trigger()).toHaveAttribute("data-light", "green"));
    expect(trigger()).toHaveTextContent("Up to date");

    await userEvent.click(trigger());
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Explorer height25,120,450");
    expect(dialog).toHaveTextContent("Node height25,120,451");
    expect(dialog).toHaveTextContent("History from block 24,998,316, older blocks still loading");
    expect(dialog).toHaveTextContent(/checked .+ ago/);
  });

  it("is red when behind, a missing height as a dash", async () => {
    renderWithProviders(<StatusLight />, { api: fakeApi({ status: async () => behind as Status }) });
    await waitFor(() => expect(trigger()).toHaveAttribute("data-light", "red"));
    expect(trigger()).toHaveTextContent("Behind");

    await userEvent.click(trigger());
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Archive height—");
    expect(dialog).toHaveTextContent("History from block 1");
    expect(dialog).not.toHaveTextContent("still loading");
  });

  it("is grey when the request fails", async () => {
    renderWithProviders(<StatusLight />, {
      api: fakeApi({ status: () => Promise.reject(new TypeError("Failed to fetch")) }),
    });
    await waitFor(() => expect(screen.getByRole("button", { name: /^Explorer status/ })).toBeInTheDocument());
    // Wait until the query has settled on its error.
    await new Promise((r) => setTimeout(r, 0));
    expect(trigger()).toHaveAttribute("data-light", "unknown");

    await userEvent.click(trigger());
    expect(await screen.findByRole("dialog")).toHaveTextContent("could not be read");
  });

  it("is keyboard accessible", async () => {
    renderWithProviders(<StatusLight />, { api: fakeApi({ status: async () => healthy as Status }) });
    await userEvent.tab();
    expect(trigger()).toHaveFocus();
    await userEvent.keyboard("{Enter}");
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
  });
});
