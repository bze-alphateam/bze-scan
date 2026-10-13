import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import ErrorPage from "@/app/error";
import NotFound from "@/app/not-found";
import { renderWithProviders } from "@/test/render";

vi.mock("next/navigation", () => ({
  usePathname: () => "/nowhere",
  useRouter: () => ({ push: vi.fn() }),
}));

describe("the not-found and error pages", () => {
  it("404 shows its code tag, the way back and a search box", () => {
    renderWithProviders(<NotFound />);
    expect(screen.getByTestId("lost-code")).toHaveTextContent("404");
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Nothing here");
    expect(screen.getByText("This page does not exist, or not yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to the explorer" })).toHaveAttribute("href", "/");
    expect(screen.getByRole("combobox", { name: "Search the explorer" })).toHaveAttribute(
      "placeholder",
      "Or search by block, transaction hash, address or validator",
    );
  });

  it("the error page tries again with reset, links back and offers a search box", async () => {
    const reset = vi.fn();
    renderWithProviders(<ErrorPage error={new Error("boom")} reset={reset} />);
    expect(screen.getByTestId("lost-code")).toHaveTextContent("error");
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Something went wrong");
    expect(screen.getByText("This page failed to load. Try again in a moment.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to the explorer" })).toHaveAttribute("href", "/");
    expect(screen.getByRole("combobox", { name: "Search the explorer" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(reset).toHaveBeenCalledOnce();
  });
});
