import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderWithProviders } from "@/test/render";

import { CardSkeleton, JsonSkeleton, ListSkeleton, TableSkeleton, TileSkeleton } from "./skeleton";

describe("skeletons", () => {
  it("TileSkeleton keeps a tile's height", () => {
    renderWithProviders(<TileSkeleton />);
    expect(screen.getByTestId("tile-skeleton")).toHaveAttribute("aria-hidden");
  });

  it("CardSkeleton renders its body lines", () => {
    renderWithProviders(<CardSkeleton lines={3} />);
    expect(screen.getByTestId("card-skeleton")).toBeInTheDocument();
  });

  it("TableSkeleton renders the asked rows and columns", () => {
    renderWithProviders(<TableSkeleton rows={5} columns={4} />);
    const rows = screen.getAllByTestId("table-skeleton-row");
    expect(rows).toHaveLength(5);
    expect(rows[0].children).toHaveLength(4);
  });

  it("ListSkeleton renders the asked rows", () => {
    renderWithProviders(<ListSkeleton rows={3} />);
    expect(screen.getAllByTestId("list-skeleton-row")).toHaveLength(3);
  });

  it("JsonSkeleton renders", () => {
    renderWithProviders(<JsonSkeleton />);
    expect(screen.getByTestId("json-skeleton")).toBeInTheDocument();
  });
});
