import { act, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithProviders } from "@/test/render";

import { TimeAgo } from "./time-ago";

const BLOCK = "2026-10-06T14:32:11Z";
const at = (seconds: number) => new Date(new Date(BLOCK).getTime() + seconds * 1000);

describe("TimeAgo", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("keeps rows mounted at different polls on the same clock", () => {
    vi.setSystemTime(at(5));
    renderWithProviders(<TimeAgo time={BLOCK} data-testid="first" />);
    expect(screen.getByTestId("first")).toHaveTextContent("5 seconds ago");

    // The next poll mounts another row for the same time 4 s later.
    act(() => vi.advanceTimersByTime(4000));
    renderWithProviders(<TimeAgo time={BLOCK} data-testid="second" />);
    expect(screen.getByTestId("first")).toHaveTextContent("9 seconds ago");
    expect(screen.getByTestId("second")).toHaveTextContent("9 seconds ago");

    act(() => vi.advanceTimersByTime(3000));
    expect(screen.getByTestId("first")).toHaveTextContent("12 seconds ago");
    expect(screen.getByTestId("second")).toHaveTextContent("12 seconds ago");
  });

  it("reads a time just ahead of the clock as just now", () => {
    vi.setSystemTime(at(-2));
    renderWithProviders(<TimeAgo time={BLOCK} />);
    expect(screen.getByText("just now")).toBeInTheDocument();
  });

  it("moves to minutes once a minute has passed", () => {
    vi.setSystemTime(at(59));
    renderWithProviders(<TimeAgo time={BLOCK} />);
    expect(screen.getByText("59 seconds ago")).toBeInTheDocument();
    act(() => vi.advanceTimersByTime(2000));
    expect(screen.getByText("1 minute ago")).toBeInTheDocument();
  });
});
