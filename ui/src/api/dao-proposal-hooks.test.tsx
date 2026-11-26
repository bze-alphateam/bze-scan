import { waitFor } from "@testing-library/react";
import type { Query } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import type { DaoProposal } from "./types";
import { fakeApi, renderWithProviders } from "@/test/render";

import { ApiError } from "./client";
import { queryKeys, useDaoProposal } from "./hooks";

import voting from "../../test/fixtures/api/v1/dao-proposals/7.json";
import executed from "../../test/fixtures/api/v1/dao-proposals/6.json";
import failed from "../../test/fixtures/api/v1/dao-proposals/5.json";

function Probe() {
  useDaoProposal(7);
  return null;
}

/** The polling interval and freshness the hook chose for what the query holds. */
async function policyFor(answer: () => Promise<DaoProposal>) {
  const { queryClient } = renderWithProviders(<Probe />, { api: fakeApi({ daoProposal: answer }) });
  await waitFor(() => expect(["success", "error"]).toContain(queryClient.getQueryState(queryKeys.daoProposal(7))?.status));
  const query = queryClient.getQueryCache().find({ queryKey: queryKeys.daoProposal(7) })!;
  const options = query.options as { refetchInterval: (q: Query) => number | false; staleTime: (q: Query) => number };
  return { interval: options.refetchInterval(query), stale: options.staleTime(query) };
}

describe("useDaoProposal polling", () => {
  it("polls while voting", async () => {
    expect((await policyFor(async () => voting as DaoProposal)).interval).toBe(4000);
  });

  it("keeps polling a failed execution, which may be retried", async () => {
    expect((await policyFor(async () => failed as DaoProposal)).interval).toBe(4000);
  });

  it("stops once the proposal is settled for good", async () => {
    const p = await policyFor(async () => executed as DaoProposal);
    expect(p.interval).toBe(false);
    expect(p.stale).toBe(Infinity);
  });

  it("stops on a 404", async () => {
    expect((await policyFor(() => Promise.reject(new ApiError(404, "not_found", "no")))).interval).toBe(false);
  });

  it("stops on a placeholder row without a status", async () => {
    expect((await policyFor(async () => ({ ...(executed as DaoProposal), status: null }))).interval).toBe(false);
  });

  it("keeps polling after a server error, which may pass", async () => {
    expect((await policyFor(() => Promise.reject(new ApiError(502, "upstream_error", "x")))).interval).toBe(4000);
  });
});
