import { describe, expect, it } from "vitest";

import { ApiError } from "./client";
import { createQueryClient, immutable, live, shouldRetry, status } from "./policy";

describe("policies", () => {
  it("holds the UI Design numbers", () => {
    expect(live).toMatchObject({ staleTime: 7000, refetchInterval: 7000, refetchIntervalInBackground: false });
    expect(immutable).toMatchObject({
      staleTime: Infinity,
      gcTime: 3_600_000,
      refetchInterval: false,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    });
    expect(status).toMatchObject({ staleTime: 60_000, refetchInterval: 60_000 });
  });

  it("never retries a 4xx", () => {
    expect(shouldRetry(0, new ApiError(404, "not_found", ""))).toBe(false);
    expect(shouldRetry(0, new ApiError(400, "bad_request", ""))).toBe(false);
  });

  it("retries other failures twice", () => {
    expect(shouldRetry(0, new ApiError(502, "upstream_error", ""))).toBe(true);
    expect(shouldRetry(1, new TypeError("Failed to fetch"))).toBe(true);
    expect(shouldRetry(2, new TypeError("Failed to fetch"))).toBe(false);
  });

  it("is the query client's default", () => {
    expect(createQueryClient().getDefaultOptions().queries?.retry).toBe(shouldRetry);
  });
});
