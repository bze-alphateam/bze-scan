import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { appIconsIn, renderWithProviders } from "@/test/render";

import { AppsMenu } from "./apps-menu";

describe("AppsMenu", () => {
  it("lists the BZE apps, each with its icon in front of the name", async () => {
    renderWithProviders(<AppsMenu />);
    await userEvent.click(screen.getByRole("button", { name: "Apps" }));

    const items = await screen.findAllByRole("menuitem");
    expect(items.map((i) => i.textContent)).toEqual(["Website", "Staking", "DEX", "Burner", "Factory", "Communities", "Docs"]);
    expect(items.map((i) => appIconsIn(i))).toEqual([["website"], ["staking"], ["dex"], ["burner"], ["factory"], ["communities"], ["docs"]]);
    // The icon is decorative: the name alone labels the link.
    expect(screen.getByRole("menuitem", { name: "Staking" })).toHaveAttribute("href", "https://staking.getbze.com");
  });
});
