import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { fakeApi, renderWithProviders } from "@/test/render";

import { Footer } from "./footer";

describe("Footer", () => {
  it("links the emergency status page, and keeps it out of the main nav", () => {
    renderWithProviders(<Footer />, { api: fakeApi({ status: async () => ({}) as never }) });
    expect(screen.getByRole("link", { name: "Emergency status" })).toHaveAttribute("href", "/emergency");
  });
});
