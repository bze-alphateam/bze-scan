"use client";

import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { isNotFound } from "./client";
import { immutable, live, reference, status, TX_PENDING_POLL_MS, TX_PENDING_WINDOW_MS } from "./policy";
import { useApi } from "./provider";
import type { ExplorerApi } from "./client";
import type {
  Page,
  PageParams,
  Proposal,
  ProposalDeposit,
  ProposalStatus,
  ProposalVote,
  TokenEvent,
  TokenListParams,
  TokenSummary,
  TxListParams,
  ValidatorList,
  ValidatorListParams,
  VoteOption,
} from "./types";

/** Query keys, one tuple per route and its arguments. */
export const queryKeys = {
  status: () => ["status"] as const,
  blocks: (p: PageParams) => ["blocks", p] as const,
  block: (height: number) => ["block", height] as const,
  blockEvents: (height: number, p: PageParams) => ["block-events", height, p] as const,
  transactions: (p: TxListParams) => ["txs", p] as const,
  transaction: (hash: string) => ["tx", hash.toUpperCase()] as const,
  validators: (p: ValidatorListParams) => ["validators", p] as const,
  allValidators: () => ["validators", "all-pages"] as const,
  validator: (operator: string) => ["validator", operator] as const,
  validatorBlocks: (operator: string, p: PageParams) => ["validator-blocks", operator, p] as const,
  account: (address: string) => ["account", address] as const,
  tokens: (p: TokenListParams) => ["tokens", p] as const,
  validatorDirectory: () => ["validator-directory"] as const,
  tokenDirectory: () => ["token-directory"] as const,
  denoms: () => ["tokens", "all-pages"] as const,
  allTokens: () => ["tokens", "all-pages", "live"] as const,
  token: (denom: string) => ["token", denom] as const,
  tokenEventsAfter: (denom: string, cursor: string) => ["token-events", denom, cursor] as const,
  tokenHolders: (denom: string) => ["token-holders", denom] as const,
  tokenTransfers: (denom: string) => ["token-transfers", denom] as const,
  proposals: (status: ProposalStatus | null) => ["proposals", status] as const,
  proposal: (id: number) => ["proposal", id] as const,
  proposalVotes: (id: number, option: VoteOption | "weighted" | null) => ["proposal-votes", id, option] as const,
  validatorVotes: (id: number) => ["proposal-votes", id, "validators"] as const,
  proposalDeposits: (id: number) => ["proposal-deposits", id] as const,
  search: (q: string) => ["search", q] as const,
  stats: () => ["stats"] as const,
  params: () => ["params"] as const,
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

/**
 * Every validator, all statuses, with the list's summary: pages of 100
 * followed to the last (the chain has a few dozen, so one page in practice).
 */
export function useAllValidators() {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.allValidators(),
    queryFn: async ({ signal }): Promise<ValidatorList> => {
      const first = await api.validators({ status: "all", limit: 100 }, signal);
      const items = [...first.items];
      let cursor = first.next_cursor;
      while (cursor) {
        const next = await api.validators({ status: "all", limit: 100, cursor }, signal);
        items.push(...next.items);
        cursor = next.next_cursor;
      }
      return { items, next_cursor: null, summary: first.summary };
    },
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

/** Every page of the tokens list, pages of 100 followed to the last. */
async function allTokenPages(api: ExplorerApi, signal: AbortSignal): Promise<TokenSummary[]> {
  const items: TokenSummary[] = [];
  let cursor: string | undefined;
  do {
    const page = await api.tokens({ limit: 100, cursor }, signal);
    items.push(...page.items);
    cursor = page.next_cursor ?? undefined;
  } while (cursor);
  return items;
}

/**
 * Every denom the explorer knows, by denom: the tokens list's pages of 100
 * followed to the last, for joining balances to symbols, logos and prices.
 */
export function useDenoms() {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.denoms(),
    queryFn: async ({ signal }) => new Map((await allTokenPages(api, signal)).map((t) => [t.denom, t])),
    // Fresh for a minute: prices move, but a balance's value need not be live.
    ...reference,
  });
}

/** The tokens page: every token, polled (the kinds are filtered on the page, so the tiles count them all). */
export function useAllTokens() {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.allTokens(),
    queryFn: ({ signal }) => allTokenPages(api, signal),
    ...live,
  });
}

export function useToken(denom: string) {
  const api = useApi();
  return useQuery({ queryKey: queryKeys.token(denom), queryFn: ({ signal }) => api.token(denom, signal), ...live });
}

/**
 * A token's events older than the token page's 20, every page from `cursor`
 * on (a denom's history is short: mints, burns, admin changes); disabled
 * without a cursor. Older events never change.
 */
export function useTokenEventsAfter(denom: string, cursor: string | null) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.tokenEventsAfter(denom, cursor ?? ""),
    queryFn: async ({ signal }) => {
      const items: TokenEvent[] = [];
      let next: string | null = cursor;
      while (next) {
        const page: Page<TokenEvent> = await api.tokenEvents(denom, { limit: 100, cursor: next }, signal);
        items.push(...page.items);
        next = page.next_cursor;
      }
      return items;
    },
    enabled: cursor != null,
    ...immutable,
  });
}

/** The page sizes of a list the page grows on request: a short first page, then larger ones. */
const firstPage = (limit: number): PageParams => ({ limit });
const nextPage =
  (limit: number) =>
  (last: Page<unknown>): PageParams | undefined =>
    last.next_cursor ? { cursor: last.next_cursor, limit } : undefined;

/** A token's holders, largest first: the top 10, then 100 a page on request. The snapshot is hourly. */
export function useTokenHolders(denom: string) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.tokenHolders(denom),
    queryFn: ({ pageParam, signal }) => api.tokenHolders(denom, pageParam, signal),
    initialPageParam: firstPage(10),
    getNextPageParam: nextPage(100),
    ...reference,
  });
}

/** A token's transfers, newest first: 20 a page, polled. */
export function useTokenTransfers(denom: string) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.tokenTransfers(denom),
    queryFn: ({ pageParam, signal }) => api.tokenTransfers(denom, pageParam, signal),
    initialPageParam: firstPage(20),
    getNextPageParam: nextPage(20),
    ...live,
  });
}

/** Every page of a list route, pages of 100 followed to the last. */
async function allPages<T>(read: (cursor: string | undefined) => Promise<Page<T>>): Promise<T[]> {
  const items: T[] = [];
  let cursor: string | undefined;
  do {
    const page = await read(cursor);
    items.push(...page.items);
    cursor = page.next_cursor ?? undefined;
  } while (cursor);
  return items;
}

/** The proposals, newest first, of one status (every status for null): 20 a page, the loaded pages polled. */
export function useProposals(status: ProposalStatus | null) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.proposals(status),
    queryFn: ({ pageParam, signal }) => api.proposals({ ...pageParam, status: status ?? undefined }, signal),
    initialPageParam: firstPage(20),
    getNextPageParam: nextPage(20),
    ...live,
  });
}

/** A proposal still moves while it collects deposits or votes; once resolved it never changes. */
const resolved = (p: Proposal | undefined) => p != null && p.status !== "deposit_period" && p.status !== "voting_period";

/** A proposal: polled while open, fetched once after its resolution. */
export function useProposal(id: number) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.proposal(id),
    queryFn: ({ signal }) => api.proposal(id, signal),
    ...live,
    refetchInterval: (q) => (resolved(q.state.data) ? false : live.refetchInterval),
    staleTime: (q) => (resolved(q.state.data) ? Infinity : live.staleTime),
  });
}

/** Everyone's votes on a proposal, newest first, of one option (every vote for null): 25 a page. */
export function useProposalVotes(id: number, option: VoteOption | "weighted" | null, open = true) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.proposalVotes(id, option),
    queryFn: ({ pageParam, signal }) => api.proposalVotes(id, { ...pageParam, option: option ?? undefined }, signal),
    initialPageParam: firstPage(25),
    getNextPageParam: nextPage(25),
    ...(open ? live : immutable),
  });
}

/** Every vote of a validator owner on a proposal (a few dozen at most); polled while voting is open. */
export function useValidatorVotes(id: number, open = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.validatorVotes(id),
    queryFn: async ({ signal }): Promise<ProposalVote[]> =>
      (await allPages((cursor) => api.proposalVotes(id, { voter: "validators", limit: 100, cursor }, signal))).filter(
        (v) => v.validator_operator != null,
      ),
    ...(open ? live : immutable),
  });
}

/** Every deposit on a proposal: a proposal has a handful. */
export function useProposalDeposits(id: number) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.proposalDeposits(id),
    queryFn: ({ signal }): Promise<ProposalDeposit[]> =>
      allPages((cursor) => api.proposalDeposits(id, { limit: 100, cursor }, signal)),
    ...reference,
  });
}

/** The home tiles: chain height, block time, supply and staking. */
export function useStats() {
  const api = useApi();
  return useQuery({ queryKey: queryKeys.stats(), queryFn: ({ signal }) => api.stats(signal), ...live });
}

/** Every module's live parameters: they change only through governance. */
export function useParams() {
  const api = useApi();
  return useQuery({ queryKey: queryKeys.params(), queryFn: ({ signal }) => api.params(signal), ...reference });
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
