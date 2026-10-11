import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { renderWithProviders } from "@/test/render";

import { ThemeToggle } from "./theme-toggle";

describe("ThemeToggle", () => {
  it("starts dark and switches to light and back, remembering the choice", async () => {
    renderWithProviders(<ThemeToggle />);

    const toLight = await screen.findByRole("button", { name: "Switch to light mode" });
    expect(document.documentElement).toHaveClass("dark");

    await userEvent.click(toLight);
    expect(await screen.findByRole("button", { name: "Switch to dark mode" })).toBeInTheDocument();
    expect(document.documentElement).toHaveClass("light");
    expect(window.localStorage.getItem("theme")).toBe("light");

    await userEvent.click(screen.getByRole("button", { name: "Switch to dark mode" }));
    expect(await screen.findByRole("button", { name: "Switch to light mode" })).toBeInTheDocument();
    expect(window.localStorage.getItem("theme")).toBe("dark");
  });
});
