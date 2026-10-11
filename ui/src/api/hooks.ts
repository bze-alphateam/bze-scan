"use client";

import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { isNotFound } from "./client";
import { immutable, live, reference, status, TX_PENDING_POLL_MS, TX_PENDING_WINDOW_MS } from "./policy";
import { useApi } from "./provider";
import type { Page, PageParams, TokenListParams, TokenSummary, TxListParams, ValidatorList, ValidatorListParams } from "./types";

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
  tokens: (p: TokenListParams) => ["tokens", p] as const,
  validatorDirectory: () => ["validator-directory"] as const,
  tokenDirectory: () => ["token-directory"] as const,
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

/**
 * Where a transaction the API answers 404 for stands: `looking` while it is
 * asked for again every 7 s, `not_found` once the pending window is over;
 * null when it is not a 404.
 */
export type PendingState = "looking" | "not_found" | null;

/**
 * A transaction: immutable once found. A 404 means "not indexed yet": the
 * query is polled every 7 s for the pending window, then given up on.
 * Mount it under a `key` of the hash so the window restarts per hash.
 */
export function useTransaction(hash: string) {
  const api = useApi();
  const [expired, setExpired] = useState(false);
  const query = useQuery({
    queryKey: queryKeys.transaction(hash),
    queryFn: ({ signal }) => api.transaction(hash.toUpperCase(), signal),
    ...immutable,
    refetchInterval: (q) => (isNotFound(q.state.error) && !expired ? TX_PENDING_POLL_MS : false),
  });
  const notFound = isNotFound(query.error);

  useEffect(() => {
    if (!notFound) return;
    const id = setTimeout(() => setExpired(true), TX_PENDING_WINDOW_MS);
    return () => clearTimeout(id);
  }, [notFound]);

  const pending: PendingState = notFound ? (expired ? "not_found" : "looking") : null;
  return { ...query, pending };
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

export function useTokens(params: TokenListParams = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.tokens(params),
    queryFn: ({ signal }) => api.tokens(params, signal),
    ...listPolicy(params),
  });
}

// Module-level selects keep their result stable between renders.
const byOperator = (list: ValidatorList) => new Map(list.items.map((v) => [v.operator_address, v]));
const byDenom = (page: Page<TokenSummary>) => new Map(page.items.map((t) => [t.denom, t]));

/** Every validator's moniker by operator address (the first 100, all statuses), for naming them on pages. */
export function useValidatorDirectory() {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.validatorDirectory(),
    queryFn: ({ signal }) => api.validators({ status: "all", limit: 100 }, signal),
    select: byOperator,
    ...reference,
  });
}

/** The tokens list's first 100 denoms by denom, for symbols, exponents and USD prices. */
export function useTokenDirectory() {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.tokenDirectory(),
    queryFn: ({ signal }) => api.tokens({ limit: 100 }, signal),
    select: byDenom,
    ...reference,
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
