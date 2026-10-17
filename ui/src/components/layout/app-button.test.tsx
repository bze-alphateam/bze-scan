import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { appIconsIn, renderWithProviders } from "@/test/render";

import { AppButton } from "./app-button";

describe("AppButton", () => {
  it("opens the app in a new tab, with its icon, outlined in its colour", () => {
    renderWithProviders(
      <AppButton app="staking" href="https://staking.getbze.com/?stake=x">
        Stake with this validator
      </AppButton>,
    );
    const link = screen.getByRole("link", { name: "Stake with this validator" });
    expect(link).toHaveAttribute("href", "https://staking.getbze.com/?stake=x");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(appIconsIn(link)).toEqual(["staking"]);
    // The default dark theme: the staking light end on the border and the label.
    const style = getComputedStyle(link);
    expect(style.borderColor).toBe("rgb(181, 126, 220)");
    expect(style.color).toBe("rgb(181, 126, 220)");
  });
});
