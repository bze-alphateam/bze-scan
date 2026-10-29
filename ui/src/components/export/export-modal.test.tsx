import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { ExportDefinition, ExportPage } from "@/lib/export";
import type { RateLimiter } from "@/lib/rate-limit";
import { renderWithProviders } from "@/test/render";

import { ExportButton, ExportModal } from "./export-modal";

interface Row {
  n: number;
  kind: string;
}

/** A list of `total` rows served 10 a read; every third row is "block". */
function definition(fetchPage: (cursor: string | null) => Promise<ExportPage<Row>>): ExportDefinition<Row, Row> {
  return {
    title: "Export rows",
    about: "You get a CSV file with the rows, newest first.",
    noun: "rows",
    fields: [
      { key: "n", header: "Number", value: (r) => r.n },
      { key: "kind", header: "Kind", value: (r) => r.kind },
      { key: "note", header: "Note", value: () => "a, b", defaultOn: false },
    ],
    filters: [
      { kind: "select", key: "kind", label: "Kind", options: [{ label: "All", value: "" }, { label: "Tx", value: "tx" }] },
      { kind: "checkbox", key: "block", label: "Include block rows", hint: "Rows the chain wrote." },
    ],
    initialFilters: { kind: "", block: true },
    limit: { max: 50, initial: 50 },
    validate: (f) => (f.kind === "tx" && f.block ? "Block rows are never of kind tx." : null),
    source: (f) => ({ fetchPage: (cursor) => fetchPage(cursor), keep: f.block ? undefined : (r) => r.kind !== "block" }),
    lines: (r) => [r],
    fileName: () => "rows.csv",
  };
}

function pages(total: number) {
  return vi.fn(async (cursor: string | null): Promise<ExportPage<Row>> => {
    const from = cursor == null ? 0 : Number(cursor);
    const items = Array.from({ length: Math.min(10, total - from) }, (_, i) => ({ n: from + i + 1, kind: (from + i + 1) % 3 === 0 ? "block" : "tx" }));
    return { items, next: from + 10 < total ? String(from + 10) : null, total };
  });
}

const free: () => RateLimiter = () => ({ wait: async () => {} });

function open(fetchPage = pages(25), save = vi.fn(), createLimiter = free) {
  const onClose = vi.fn();
  renderWithProviders(<ExportModal open onClose={onClose} definition={definition(fetchPage)} createLimiter={createLimiter} save={save} />);
  return { fetchPage, save, onClose, user: userEvent.setup() };
}

describe("ExportModal", () => {
  it("explains the file, lists the columns (default ones ticked), the filters and the limit", async () => {
    open();
    const modal = await screen.findByTestId("export-modal");
    expect(within(modal).getByRole("heading", { name: "Export rows" })).toBeDefined();
    expect(screen.getByTestId("export-about").textContent).toContain("You get a CSV file with the rows, newest first.");
    expect((screen.getByRole("checkbox", { name: "Number" }) as HTMLInputElement).checked).toBe(true);
    expect((screen.getByRole("checkbox", { name: "Note" }) as HTMLInputElement).checked).toBe(false);
    expect((screen.getByRole("checkbox", { name: /Include block rows/ }) as HTMLInputElement).checked).toBe(true);
    expect((screen.getByRole("combobox", { name: "Kind" }) as HTMLSelectElement).value).toBe("");
    expect((screen.getByRole("textbox", { name: "How many rows at most" }) as HTMLInputElement).value).toBe("50");
  });

  it("reads every page, then downloads the chosen columns as CSV", async () => {
    const { fetchPage, save, user } = open();
    await user.click(await screen.findByRole("checkbox", { name: "Note" }));
    await user.click(screen.getByRole("checkbox", { name: "Kind" }));
    await user.click(screen.getByRole("button", { name: "Export" }));
    await waitFor(() => expect(screen.getByTestId("export-status").textContent).toBe("Ready: 25 rows, 25 lines."));
    expect(fetchPage).toHaveBeenCalledTimes(3);

    await user.click(screen.getByRole("button", { name: /Download CSV/ }));
    expect(save).toHaveBeenCalledTimes(1);
    const [name, text] = save.mock.calls[0] as [string, string];
    expect(name).toBe("rows.csv");
    const lines = text.split("\r\n");
    expect(lines[0]).toBe("Number,Note");
    expect(lines[1]).toBe('1,"a, b"');
    expect(lines).toHaveLength(27);
  });

  it("applies the filters and the limit", async () => {
    const { save, user } = open(pages(100));
    await user.click(await screen.findByRole("checkbox", { name: /Include block rows/ }));
    const limit = screen.getByRole("textbox", { name: "How many rows at most" });
    await user.clear(limit);
    await user.type(limit, "12");
    await user.click(screen.getByRole("button", { name: "Export" }));
    await waitFor(() => expect(screen.getByTestId("export-status").textContent).toBe("Ready: 12 rows, 12 lines."));
    await user.click(screen.getByRole("button", { name: /Download CSV/ }));
    const text = save.mock.calls[0][1] as string;
    expect(text).not.toContain("block");
  });

  it("refuses a bad limit, a filter error or no columns", async () => {
    const { user } = open();
    const exportButton = await screen.findByRole("button", { name: "Export" });
    const limit = screen.getByRole("textbox", { name: "How many rows at most" });
    await user.clear(limit);
    await user.type(limit, "51");
    expect(screen.getByText("Choose a whole number from 1 to 50.")).toBeDefined();
    expect((exportButton as HTMLButtonElement).disabled).toBe(true);
    await user.clear(limit);
    await user.type(limit, "5");
    expect((exportButton as HTMLButtonElement).disabled).toBe(false);

    await user.selectOptions(screen.getByRole("combobox", { name: "Kind" }), "tx");
    expect(screen.getByTestId("export-filter-error").textContent).toBe("Block rows are never of kind tx.");
    expect((exportButton as HTMLButtonElement).disabled).toBe(true);
    await user.selectOptions(screen.getByRole("combobox", { name: "Kind" }), "");

    await user.click(screen.getByRole("checkbox", { name: "Number" }));
    await user.click(screen.getByRole("checkbox", { name: "Kind" }));
    expect(screen.getByText("Choose at least one column.")).toBeDefined();
    expect((exportButton as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows the progress while reading and carries on after a failure with Try again", async () => {
    const fetchPage = pages(25);
    let release!: () => void;
    fetchPage.mockImplementationOnce(async (c) => {
      await new Promise<void>((r) => (release = r));
      return pages(25)(c);
    });
    fetchPage.mockImplementationOnce(pages(25)).mockRejectedValueOnce(new Error("502"));
    const { user } = open(fetchPage);
    await user.click(await screen.findByRole("button", { name: "Export" }));
    expect(screen.getByTestId("export-status").textContent).toBe("Reading… 0 rows so far.");
    expect(screen.getByRole("button", { name: "Stop" })).toBeDefined();
    release();
    await waitFor(() => expect(screen.getByTestId("export-status").textContent).toMatch(/The explorer did not answer\. 20 rows are read/));

    await user.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.getByTestId("export-status").textContent).toBe("Ready: 25 rows, 25 lines."));
    expect(fetchPage.mock.calls.map((c) => c[0])).toEqual([null, "10", "20", "20"]);
  });

  it("waits for the limiter before every read", async () => {
    const waits: number[] = [];
    let n = 0;
    const { user } = open(pages(25), vi.fn(), () => ({ wait: async () => void waits.push(++n) }));
    await user.click(await screen.findByRole("button", { name: "Export" }));
    await waitFor(() => expect(screen.getByTestId("export-status").textContent).toMatch(/^Ready/));
    expect(waits).toEqual([1, 2, 3]);
  });

  it("stops a running export and goes back to the choices", async () => {
    const fetchPage = vi.fn(() => new Promise<ExportPage<Row>>(() => {}));
    const { user } = open(fetchPage);
    await user.click(await screen.findByRole("button", { name: "Export" }));
    await user.click(screen.getByRole("button", { name: "Stop" }));
    expect(screen.getByRole("button", { name: "Export" })).toBeDefined();
    expect(screen.queryByTestId("export-progress")).toBeNull();
  });
});

describe("ExportButton", () => {
  it("builds the definition when clicked and opens the modal", async () => {
    const build = vi.fn(() => definition(pages(5)));
    renderWithProviders(<ExportButton definition={build} createLimiter={free} save={vi.fn()} />);
    expect(build).not.toHaveBeenCalled();
    await userEvent.setup().click(screen.getByRole("button", { name: "Export" }));
    expect(build).toHaveBeenCalledTimes(1);
    expect(await screen.findByTestId("export-modal")).toBeDefined();
  });
});
