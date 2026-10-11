import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithProviders } from "@/test/render";

import { Header } from "./header";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  usePathname: () => "/blocks",
  useRouter: () => ({ push }),
}));

beforeEach(() => push.mockReset());

// jsdom applies Chakra's base styles only, so the header renders its phone
// layout: the desktop row is in the DOM but hidden.
describe("Header", () => {
  it("lists the sections and marks the current one", () => {
    renderWithProviders(<Header />);
    // Hidden elements have no accessible name, so find the nav by its label.
    const nav = screen.getByLabelText("Explorer");
    const names = within(nav)
      .getAllByRole("link", { hidden: true })
      .map((a) => a.textContent);
    expect(names).toEqual(["Blocks", "Transactions", "Validators", "Tokens", "Proposals", "Parameters"]);
    expect(within(nav).getByText("Blocks")).toHaveAttribute("aria-current", "page");
    expect(within(nav).getByText("Tokens")).not.toHaveAttribute("aria-current");
  });

  it("submits a search to /search?q=", async () => {
    renderWithProviders(<Header />);
    await userEvent.click(screen.getByRole("button", { name: "Open search" }));
    await userEvent.type(screen.getByRole("searchbox", { name: "Search the explorer" }), " bze1abc {Enter}");
    expect(push).toHaveBeenCalledWith("/search?q=bze1abc");
    // Submitting closes the phone box.
    expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
  });

  it("ignores an empty search", async () => {
    renderWithProviders(<Header />);
    await userEvent.click(screen.getByRole("button", { name: "Open search" }));
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(push).not.toHaveBeenCalled();
  });

  it("opens the phone drawer with the nav and the Apps links, and closes it on navigation", async () => {
    renderWithProviders(<Header />);
    await userEvent.click(screen.getByRole("button", { name: "Open menu" }));

    const drawer = await screen.findByRole("dialog");
    expect(within(drawer).getByRole("link", { name: "Validators" })).toHaveAttribute("href", "/validators");
    expect(within(drawer).getByRole("link", { name: "DEX" })).toHaveAttribute("href", "https://dex.getbze.com");
    expect(within(drawer).getByRole("link", { name: "Docs" })).toHaveAttribute("href", "https://docs.getbze.com");

    // jsdom cannot navigate; keep the click, drop the page load.
    document.addEventListener("click", (e) => e.preventDefault(), { once: true });
    await userEvent.click(within(drawer).getByRole("link", { name: "Validators" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("opens the phone search box from its icon", async () => {
    renderWithProviders(<Header />);
    expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Open search" }));
    expect(screen.getByRole("searchbox")).toHaveFocus();
    expect(screen.getByRole("button", { name: "Close search" })).toHaveAttribute("aria-expanded", "true");
  });
});
