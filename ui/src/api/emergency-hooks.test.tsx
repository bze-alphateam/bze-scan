import { waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { fakeApi, renderWithProviders } from "@/test/render";

import { useEmergency } from "./hooks";
import { status } from "./policy";

function Probe() {
  useEmergency();
  return null;
}

describe("useEmergency", () => {
  it("polls with the status policy (60 s)", async () => {
    const { queryClient } = renderWithProviders(<Probe />, {
      api: fakeApi({ emergency: async () => ({ supported: true }) as never }),
    });
    await waitFor(() => expect(queryClient.getQueryState(["emergency"])?.status).toBe("success"));
    const options = queryClient.getQueryCache().find({ queryKey: ["emergency"] })!.options as { refetchInterval?: unknown; staleTime?: unknown };
    expect(options.refetchInterval).toBe(status.refetchInterval);
    expect(options.staleTime).toBe(status.staleTime);
    expect(status.refetchInterval).toBe(60_000);
  });
});
