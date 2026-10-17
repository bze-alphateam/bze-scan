import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderWithProviders } from "@/test/render";

import { Logo } from "./logo";

const sources = (testId: string) =>
  within(screen.getByTestId(testId))
    .getAllByRole("presentation", { hidden: true })
    .map((img) => img.getAttribute("src"));

describe("Logo", () => {
  it("links home with one accessible name and decorative images", () => {
    renderWithProviders(<Logo />);
    const link = screen.getByRole("link", { name: "BZE Explorer home" });
    expect(link).toHaveAttribute("href", "/");
    for (const img of within(link).getAllByRole("presentation", { hidden: true })) expect(img).toHaveAttribute("alt", "");
  });

  it("carries the landscape lockup for wide screens and the compact one for phones, light and dark", () => {
    renderWithProviders(<Logo />);
    expect(sources("logo-landscape")).toEqual(["/images/explorer/landscape-light.svg", "/images/explorer/landscape-dark.svg"]);
    expect(sources("logo-compact")).toEqual(["/images/explorer/compact-light.svg", "/images/explorer/compact-dark.svg"]);
  });
});
