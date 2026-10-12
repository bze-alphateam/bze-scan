import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { ValidatorList } from "@/api/types";
import { fakeApi, renderWithProviders } from "@/test/render";

import list from "../../../test/fixtures/api/v1/validators.json";

import { Amount } from "./amount";
import { Avatar } from "./avatar";
import { Card } from "./card";
import { JsonBlock } from "./json-block";
import { AddressLink, HashLink } from "./links";
import { MoreDetails } from "./more-details";
import { TimeAgo } from "./time-ago";
import { ValidatorChip } from "./validator-chip";

const ADDRESS = "bze1q8f7z6x5c4v3d2n0mlkjhgfdsapzxu4a";

describe("Amount", () => {
  it("shows display units with the symbol", () => {
    renderWithProviders(<Amount amount="1234500000" denom="ubze" symbol="BZE" exponent={6} />);
    expect(screen.getByText("1,234.5 BZE")).toBeInTheDocument();
  });

  it("shows the raw amount and denom without metadata", () => {
    renderWithProviders(<Amount amount="42" denom="factory/bze1x/uhoney" symbol={null} exponent={null} />);
    expect(screen.getByText("42 factory/bze1x/uhoney")).toBeInTheDocument();
  });

  it("puts the full value in the title of a compact amount", () => {
    renderWithProviders(<Amount amount="14300000000000" denom="ubze" symbol="BZE" exponent={6} compact />);
    expect(screen.getByText("14.3M BZE")).toHaveAttribute("title", "14,300,000 BZE");
  });
});

describe("links", () => {
  it("links a short address to its account and copies the full one", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });

    renderWithProviders(<AddressLink address={ADDRESS} />);
    expect(screen.getByRole("link", { name: "bze1q8f…xu4a" })).toHaveAttribute("href", `/accounts/${ADDRESS}`);
    await userEvent.click(screen.getByRole("button", { name: "Copy address" }));
    expect(writeText).toHaveBeenCalledWith(ADDRESS);
    expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument();
  });

  it("links a validator address to its validator page", () => {
    renderWithProviders(<AddressLink address="bzevaloper1acdefghjklmnpqrstuvwxyz0023" noCopy />);
    expect(screen.getByRole("link")).toHaveAttribute("href", "/validators/bzevaloper1acdefghjklmnpqrstuvwxyz0023");
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("shows a module account as a dashed chip", () => {
    renderWithProviders(<AddressLink address={ADDRESS} label={{ name: "Fee collector", kind: "module" }} />);
    expect(screen.getByRole("link", { name: "Fee collector · module" })).toBeInTheDocument();
  });

  it("shows another label next to the short address", () => {
    renderWithProviders(<AddressLink address={ADDRESS} label={{ name: "Osmosis escrow", kind: "ibc_escrow" }} />);
    expect(screen.getByText("Osmosis escrow")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "bze1q8f…xu4a" })).toBeInTheDocument();
  });

  it("links a short hash to its transaction", () => {
    const hash = "3f9a0000000000000000000000000000000000000000000000000000000c21e";
    renderWithProviders(<HashLink hash={hash} />);
    expect(screen.getByRole("link", { name: "3F9A…C21E" })).toHaveAttribute("href", `/txs/${hash.toUpperCase()}`);
  });
});

describe("MoreDetails", () => {
  it("mounts its content on the first open and keeps it", async () => {
    renderWithProviders(
      <MoreDetails hint="raw JSON, gas">
        <p>raw json</p>
      </MoreDetails>,
    );
    const button = screen.getByRole("button", { name: /More details · raw JSON, gas/ });
    expect(button).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("raw json")).not.toBeInTheDocument();

    await userEvent.click(button);
    expect(button).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("raw json")).toBeVisible();

    await userEvent.click(button);
    expect(screen.getByText("raw json")).not.toBeVisible();
  });
});

describe("other atoms", () => {
  it("TimeAgo shows the absolute time on hover", () => {
    renderWithProviders(<TimeAgo time="2026-10-06T14:32:11Z" />);
    const time = screen.getByText(/ago$/);
    expect(time.closest("[title]")).toHaveAttribute("title", "Oct 6, 2026, 14:32:11 UTC");
    expect(time).toHaveAttribute("datetime", "2026-10-06T14:32:11Z");
  });

  it("Avatar shows initials", () => {
    renderWithProviders(<Avatar name="Thamar Node" />);
    expect(screen.getByText("TN")).toBeInTheDocument();
  });

  it("Card shows its title, meta line and body", () => {
    renderWithProviders(
      <Card title="Overview" meta="Block 5">
        body
      </Card>,
    );
    expect(screen.getByRole("heading", { name: "Overview" })).toBeInTheDocument();
    expect(screen.getByText("Block 5")).toBeInTheDocument();
    expect(screen.getByText("body")).toBeInTheDocument();
  });

  it("JsonBlock pretty-prints with a copy button", () => {
    renderWithProviders(<JsonBlock value={{ a: 1 }} />);
    expect(screen.getByText(/"a": 1/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy JSON" })).toBeInTheDocument();
  });
});

describe("pictures", () => {
  it("shows a picture, and the initials when it fails to load", () => {
    renderWithProviders(<Avatar name="Honeycomb" src="https://pictures.example/h.jpg" />);
    const img = screen.getByTestId("avatar-image");
    expect(img).toHaveAttribute("src", "https://pictures.example/h.jpg");
    fireEvent.error(img);
    expect(screen.queryByTestId("avatar-image")).not.toBeInTheDocument();
    expect(screen.getByText("HO")).toBeInTheDocument();
  });

  it("gives a validator chip the validator's picture from the validators list", async () => {
    const op = list.items[0].operator_address;
    const withLogo = { ...list, items: list.items.map((v, i) => (i === 0 ? { ...v, logo_url: "https://pictures.example/v.jpg" } : v)) };
    renderWithProviders(<ValidatorChip operator={op} moniker="Honeycomb" />, {
      api: fakeApi({ validators: () => Promise.resolve(withLogo as ValidatorList) }),
    });
    expect(await screen.findByTestId("avatar-image")).toHaveAttribute("src", "https://pictures.example/v.jpg");
  });

  it("keeps the initials of a validator without a picture", async () => {
    renderWithProviders(<ValidatorChip operator={list.items[1].operator_address} moniker="Bee Node" />, {
      api: fakeApi({ validators: () => Promise.resolve(list as ValidatorList) }),
    });
    expect(await screen.findByText("BN")).toBeInTheDocument();
    expect(screen.queryByTestId("avatar-image")).not.toBeInTheDocument();
  });
});
