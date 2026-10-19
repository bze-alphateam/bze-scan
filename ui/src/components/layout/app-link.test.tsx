import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { appIconsIn, renderWithProviders } from "@/test/render";

import { AppIconButton, AppLink } from "./app-link";

describe("AppLink", () => {
  it("opens the app in a new tab, with its icon and its name in its accent", () => {
    renderWithProviders(
      <AppLink app="staking" href="https://staking.getbze.com/?stake=x">
        Staking
      </AppLink>,
    );
    const link = screen.getByRole("link", { name: "Staking" });
    expect(link).toHaveAttribute("href", "https://staking.getbze.com/?stake=x");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(link).toHaveAttribute("data-app", "staking");
    expect(appIconsIn(link)).toEqual(["staking"]);
    // A ghost link: the default dark theme's staking light end on the label, no border.
    const style = getComputedStyle(link);
    expect(style.color).toBe("rgb(181, 126, 220)");
    expect(style.borderWidth).not.toBe("1px");
  });

  it("uses each app's own accent", () => {
    renderWithProviders(
      <>
        <AppLink app="dex" href="https://dex.getbze.com">
          DEX
        </AppLink>
        <AppLink app="staking" href="https://staking.getbze.com">
          Staking
        </AppLink>
      </>,
    );
    const dex = getComputedStyle(screen.getByRole("link", { name: "DEX" })).color;
    const staking = getComputedStyle(screen.getByRole("link", { name: "Staking" })).color;
    expect(dex).not.toBe(staking);
  });
});

describe("AppIconButton", () => {
  it("is named by its aria-label and shows only the app icon", () => {
    renderWithProviders(<AppIconButton app="dex" href="https://dex.getbze.com" aria-label="Trade on DEX" />);
    const link = screen.getByRole("link", { name: "Trade on DEX" });
    expect(link).toHaveAttribute("title", "Trade on DEX");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(link).toHaveTextContent("");
    expect(appIconsIn(link)).toEqual(["dex"]);
  });
});
