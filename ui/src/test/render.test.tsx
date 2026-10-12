import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useBlocks } from "@/api/hooks";
import { TableSkeleton } from "@/components/ui/skeleton";

import { fakeApi, pendingApi, renderWithProviders } from "./render";

// How a page ticket asserts its first-load skeleton.
function BlocksTable() {
  const { data } = useBlocks();
  if (!data) return <TableSkeleton rows={3} columns={4} />;
  return <p>{data.pages[0].items.length} blocks</p>;
}

describe("renderWithProviders", () => {
  it("shows the skeleton while pendingApi() never answers", () => {
    renderWithProviders(<BlocksTable />, { api: pendingApi() });
    expect(screen.getByTestId("table-skeleton")).toBeInTheDocument();
  });

  it("renders the data a fake API answers", async () => {
    renderWithProviders(<BlocksTable />, { api: fakeApi({ blocks: async () => ({ items: [], next_cursor: null }) }) });
    expect(await screen.findByText("0 blocks")).toBeInTheDocument();
  });

  it("rejects calls the fake API does not implement", async () => {
    await expect(fakeApi().status()).rejects.toThrow("unexpected call to status");
  });
});
