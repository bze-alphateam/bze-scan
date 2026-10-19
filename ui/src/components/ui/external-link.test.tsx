import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderWithProviders } from "@/test/render";

import { ExternalLink } from "./external-link";

describe("ExternalLink", () => {
  it("opens in a new tab with an external mark and no typed arrow", () => {
    renderWithProviders(<ExternalLink href="https://ping.pub/beezee">Open on Ping.pub</ExternalLink>);
    const link = screen.getByRole("link", { name: "Open on Ping.pub" });
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(link.querySelector("svg[aria-hidden]")).not.toBeNull();
    expect(link.textContent).toBe("Open on Ping.pub");
  });

  it("adds nofollow only for user content", () => {
    renderWithProviders(
      <ExternalLink href="https://validator.example" userContent>
        validator.example
      </ExternalLink>,
    );
    expect(screen.getByRole("link", { name: "validator.example" })).toHaveAttribute("rel", "noopener noreferrer nofollow");
  });
});
