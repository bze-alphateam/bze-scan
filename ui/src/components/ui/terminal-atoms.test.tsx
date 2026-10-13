import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LuSearchX } from "react-icons/lu";

import { renderWithProviders } from "@/test/render";

import { AccountChip } from "./account-chip";
import { Avatar } from "./avatar";
import { Chip, type ChipVariant } from "./chip";
import { StateBox } from "./state-box";
import { StatusPill, Tag } from "./status-pill";

const ACCOUNT = "bze1dteqx7wq6m3r5k0c8jkz9hspufz2w4qyfx4f";

describe("Chip", () => {
  it("renders every variant with its text, the module one with a lock", () => {
    const variants: ChipVariant[] = ["default", "ok", "acc", "warn", "bad", "ch", "via", "ln", "hist", "modc"];
    renderWithProviders(
      <>
        {variants.map((v) => (
          <Chip key={v} variant={v}>
            {`chip ${v}`}
          </Chip>
        ))}
      </>,
    );
    for (const v of variants) {
      expect(screen.getByText(`chip ${v}`)).toHaveAttribute("data-chip", v);
    }
    expect(screen.getByText("chip modc").querySelector("svg")).not.toBeNull();
    expect(screen.getByText("chip ch").querySelector("svg")).toBeNull();
  });
});

describe("Tag", () => {
  it("shows a status with its tone", () => {
    renderWithProviders(
      <>
        <Tag tone="warn">Voting</Tag>
        <StatusPill success />
        <StatusPill success={false} />
      </>,
    );
    expect(screen.getByText("Voting")).toHaveAttribute("data-tag", "warn");
    expect(screen.getByText("Success")).toHaveAttribute("data-tag", "ok");
    expect(screen.getByText("Failed")).toHaveAttribute("data-tag", "bad");
  });
});

describe("StateBox", () => {
  it("shows the title, the line and the actions in its tone", () => {
    renderWithProviders(
      <StateBox tone="error" icon={<LuSearchX />} title="This block could not be loaded" actions={<button type="button">Try again</button>}>
        The explorer did not answer.
      </StateBox>,
    );
    const title = screen.getByRole("heading", { name: "This block could not be loaded" });
    const box = title.closest("[data-state]");
    expect(box).toHaveAttribute("data-state", "error");
    expect(box).toHaveTextContent("The explorer did not answer.");
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("defaults to the neutral tone without a line or actions", () => {
    renderWithProviders(<StateBox icon={<LuSearchX />} title="Nothing here" />);
    const box = screen.getByRole("heading", { name: "Nothing here" }).closest("[data-state]");
    expect(box).toHaveAttribute("data-state", "neutral");
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});

describe("Avatar variants", () => {
  it("draws initials, a dashed module avatar and a round token avatar", () => {
    renderWithProviders(
      <>
        <Avatar name="Thamar Node" />
        <Avatar name="Fee collector" variant="module" />
        <Avatar name="Honey" variant="token" />
      </>,
    );
    expect(screen.getByText("TN")).toHaveAttribute("data-avatar", "default");
    expect(screen.getByText("FC")).toHaveAttribute("data-avatar", "module");
    expect(screen.getByText("HO")).toHaveAttribute("data-avatar", "token");
  });

  it("puts the gold ring around the initials and around a picture", () => {
    renderWithProviders(
      <>
        <Avatar name="Thamar Node" ring="gold" />
        <Avatar name="Honeycomb" src="https://pictures.example/h.jpg" ring="gold" />
      </>,
    );
    expect(screen.getByText("TN").parentElement).toHaveAttribute("data-ring", "gold");
    expect(screen.getByTestId("avatar-image").parentElement).toHaveAttribute("data-ring", "gold");
  });

  it("rounds pictures, the gold ring with them, and keeps the initials square", () => {
    renderWithProviders(
      <>
        <Avatar name="Honeycomb" src="https://pictures.example/h.jpg" ring="gold" />
        <Avatar name="Thamar Node" src="https://pictures.example/t.jpg" />
        <Avatar name="Pluton Node" ring="gold" />
      </>,
    );
    const [ringed, plain] = screen.getAllByTestId("avatar-image");
    const round = "var(--chakra-radii-full)";
    expect(ringed).toHaveStyle({ borderRadius: round });
    expect(ringed.parentElement).toHaveStyle({ borderRadius: round });
    expect(plain).toHaveStyle({ borderRadius: round });
    expect(screen.getByText("PN")).not.toHaveStyle({ borderRadius: round });
    expect(screen.getByText("PN").parentElement).not.toHaveStyle({ borderRadius: round });
  });
});

describe("AccountChip", () => {
  it("shortens the address to the prefix, three characters and the last four, the full one on hover", () => {
    renderWithProviders(<AccountChip address={ACCOUNT} />);
    const link = screen.getByRole("link", { name: "bze1dte…fx4f" });
    expect(link).toHaveAttribute("title", ACCOUNT);
    expect(link.querySelector("[data-avatar]")).toHaveAttribute("data-avatar", "default");
  });

  it("gives a module account the dashed avatar", () => {
    renderWithProviders(<AccountChip address={ACCOUNT} label={{ name: "Fee collector", kind: "module" }} />);
    const link = screen.getByRole("link", { name: "Fee collector · module" });
    expect(link.querySelector("[data-avatar]")).toHaveAttribute("data-avatar", "module");
  });
});
