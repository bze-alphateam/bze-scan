"use client";

import { useQuery } from "@tanstack/react-query";

import { immutable, live, status } from "./policy";
import { useApi } from "./provider";
import type { PageParams, TxListParams, ValidatorListParams } from "./types";

/** Query keys, one tuple per route and its arguments. */
export const queryKeys = {
  status: () => ["status"] as const,
  blocks: (p: PageParams) => ["blocks", p] as const,
  block: (height: number) => ["block", height] as const,
  blockEvents: (height: number, p: PageParams) => ["block-events", height, p] as const,
  transactions: (p: TxListParams) => ["txs", p] as const,
  transaction: (hash: string) => ["tx", hash.toUpperCase()] as const,
  validators: (p: ValidatorListParams) => ["validators", p] as const,
  validator: (operator: string) => ["validator", operator] as const,
  validatorBlocks: (operator: string, p: PageParams) => ["validator-blocks", operator, p] as const,
  account: (address: string) => ["account", address] as const,
  search: (q: string) => ["search", q] as const,
  rawBlock: (height: number) => ["raw", "block", height] as const,
  rawBlockResults: (height: number) => ["raw", "block_results", height] as const,
  rawCommit: (height: number) => ["raw", "commit", height] as const,
  rawTx: (hash: string) => ["raw", "tx", hash.toUpperCase()] as const,
};

// A list's first page is live; an older page (with a cursor) never changes.
const listPolicy = (p: PageParams) => (p.cursor ? immutable : live);

export function useStatus() {
  const api = useApi();
  return useQuery({ queryKey: queryKeys.status(), queryFn: ({ signal }) => api.status(signal), ...status });
}

export function useBlocks(params: PageParams = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.blocks(params),
    queryFn: ({ signal }) => api.blocks(params, signal),
    ...listPolicy(params),
  });
}

export function useBlock(height: number) {
  const api = useApi();
  return useQuery({ queryKey: queryKeys.block(height), queryFn: ({ signal }) => api.block(height, signal), ...immutable });
}

export function useBlockEvents(height: number, params: PageParams = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.blockEvents(height, params),
    queryFn: ({ signal }) => api.blockEvents(height, params, signal),
    ...immutable,
  });
}

export function useTransactions(params: TxListParams = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.transactions(params),
    queryFn: ({ signal }) => api.transactions(params, signal),
    ...listPolicy(params),
  });
}

export function useTransaction(hash: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.transaction(hash),
    queryFn: ({ signal }) => api.transaction(hash, signal),
    ...immutable,
  });
}

export function useValidators(params: ValidatorListParams = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.validators(params),
    queryFn: ({ signal }) => api.validators(params, signal),
    ...live,
  });
}

export function useValidator(operator: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.validator(operator),
    queryFn: ({ signal }) => api.validator(operator, signal),
    ...live,
  });
}

export function useValidatorBlocks(operator: string, params: PageParams = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.validatorBlocks(operator, params),
    queryFn: ({ signal }) => api.validatorBlocks(operator, params, signal),
    ...listPolicy(params),
  });
}

export function useAccount(address: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.account(address),
    queryFn: ({ signal }) => api.account(address, signal),
    ...live,
  });
}

/** Runs only for a non-empty query. */
export function useSearch(q: string) {
  const api = useApi();
  const query = q.trim();
  return useQuery({
    queryKey: queryKeys.search(query),
    queryFn: ({ signal }) => api.search(query, signal),
    enabled: query !== "",
    ...live,
  });
}

export function useRawBlock(height: number, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rawBlock(height),
    queryFn: ({ signal }) => api.rawBlock(height, signal),
    enabled,
    ...immutable,
  });
}

export function useRawBlockResults(height: number, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rawBlockResults(height),
    queryFn: ({ signal }) => api.rawBlockResults(height, signal),
    enabled,
    ...immutable,
  });
}

export function useRawCommit(height: number, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rawCommit(height),
    queryFn: ({ signal }) => api.rawCommit(height, signal),
    enabled,
    ...immutable,
  });
}

export function useRawTx(hash: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rawTx(hash),
    queryFn: ({ signal }) => api.rawTx(hash, signal),
    enabled,
    ...immutable,
  });
}
