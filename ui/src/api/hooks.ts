"use client";

import { useInfiniteQuery, useQueries, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { isNotFound } from "./client";
import { immutable, live, reference, status, TX_PENDING_POLL_MS, TX_PENDING_WINDOW_MS } from "./policy";
import { useApi } from "./provider";
import type { ExplorerApi } from "./client";
import type {
  ActivityCategory,
  BlockListParams,
  BlockTransfer,
  HallOfFame,
  Page,
  PageParams,
  Proposal,
  ProposalDeposit,
  ProposalStatus,
  ProposalVote,
  TokenEvent,
  TokenListParams,
  TokenSummary,
  Tx,
  TxListParams,
  ValidatorList,
  ValidatorListParams,
  VoteOption,
} from "./types";

/** Query keys, one tuple per route and its arguments. */
export const queryKeys = {
  status: () => ["status"] as const,
  blocks: (p: BlockListParams) => ["blocks", p] as const,
  block: (height: number) => ["block", height] as const,
  blockTransfers: (height: number) => ["block-transfers", height] as const,
  blockEvents: (height: number, p: PageParams) => ["block-events", height, p] as const,
  transactions: (p: TxListParams) => ["txs", p] as const,
  transaction: (hash: string) => ["tx", hash.toUpperCase()] as const,
  validators: (p: ValidatorListParams) => ["validators", p] as const,
  allValidators: () => ["validators", "all-pages"] as const,
  hallOfFame: () => ["validators", "hall-of-fame"] as const,
  validator: (operator: string) => ["validator", operator] as const,
  validatorBlocks: (operator: string, p: PageParams) => ["validator-blocks", operator, p] as const,
  account: (address: string) => ["account", address] as const,
  accountActivity: (address: string, category: ActivityCategory | null, page: number, limit: number) =>
    ["account-activity", address, category ?? "all", page, limit] as const,
  accountIbc: (address: string) => ["account-ibc", address] as const,
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
  proposalNonVoters: (id: number) => ["proposal-non-voters", id] as const,
  search: (q: string) => ["search", q] as const,
  stats: () => ["stats"] as const,
  params: () => ["params"] as const,
  modules: () => ["modules"] as const,
  rawBlock: (height: number) => ["raw", "block", height] as const,
  rawBlockResults: (height: number) => ["raw", "block_results", height] as const,
  rawCommit: (height: number) => ["raw", "commit", height] as const,
  rawTx: (hash: string) => ["raw", "tx", hash.toUpperCase()] as const,
};

// A list's first page is live; an older page (with a cursor, or below a height) never changes.
const listPolicy = (p: BlockListParams) => (p.cursor || p.before ? immutable : live);

export function useStatus() {
  const api = useApi();
  return useQuery({ queryKey: queryKeys.status(), queryFn: ({ signal }) => api.status(signal), ...status });
}

/** One page of blocks: the latest (the home page's five, the list's live page) live, a page `before` a height immutable. */
export function useBlocks(params: BlockListParams = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.blocks(params),
    queryFn: ({ signal }) => api.blocks(params, signal),
    ...listPolicy(params),
  });
}

/**
 * The query's error, with a 404 held through the refetches that poll for it.
 * A refetch of a query that never had data clears its error while in flight,
 * so without this a page waiting for a transaction or height would flash its
 * loading state on every poll, and the pending window would restart each time.
 */
function useHeldNotFound(error: unknown, hasData: boolean): unknown {
  const [held, setHeld] = useState<unknown>(null);
  if (isNotFound(error) && held !== error) setHeld(error);
  if (hasData && held !== null) setHeld(null);
  return error ?? (hasData ? null : held);
}

/** Sets `expired` once `active` has held for the pending window (two minutes). */
function usePendingWindow(active: boolean, setExpired: (expired: boolean) => void) {
  useEffect(() => {
    if (!active) return;
    const id = setTimeout(() => setExpired(true), TX_PENDING_WINDOW_MS);
    return () => clearTimeout(id);
  }, [active, setExpired]);
}

/**
 * A block: immutable once found. With `waitForIt` (a height above the
 * explorer's latest block) a 404 is asked for again every 4 s for the pending
 * window, as a pending transaction is; `expired` says the window is over.
 * Mount it under a `key` of the height so the window restarts per height.
 */
export function useBlock(height: number, { waitForIt = false }: { waitForIt?: boolean } = {}) {
  const api = useApi();
  const [expired, setExpired] = useState(false);
  const query = useQuery({
    queryKey: queryKeys.block(height),
    queryFn: ({ signal }) => api.block(height, signal),
    ...immutable,
    refetchInterval: (q) => (waitForIt && isNotFound(q.state.error) && !expired ? TX_PENDING_POLL_MS : false),
  });
  const error = useHeldNotFound(query.error, query.data !== undefined);
  usePendingWindow(waitForIt && isNotFound(error), setExpired);
  return { ...query, error, expired };
}

/** Every transfer at a height, pages of 100 followed to the last; fetched once. */
export function useBlockTransfers(height: number, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.blockTransfers(height),
    queryFn: ({ signal }): Promise<BlockTransfer[]> =>
      allPages((cursor) => api.blockTransfers(height, { limit: 100, cursor }, signal)),
    enabled,
    ...immutable,
  });
}

export function useBlockEvents(height: number, params: PageParams = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.blockEvents(height, params),
    queryFn: ({ signal }) => api.blockEvents(height, params, signal),
    ...immutable,
  });
}

/** One page of transactions: the latest (home, the list's live page) live, a page after a cursor immutable. */
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
 * asked for again every 4 s, `not_found` once the pending window is over;
 * null when it is not a 404.
 */
export type PendingState = "looking" | "not_found" | null;

/**
 * Whether a found transaction can still change: the order book has not
 * processed its DEX messages yet, or a cross-chain transfer it created waits
 * for the relayer (an expired one waits for a refund that may never come).
 */
export function isUnsettled(tx: Tx): boolean {
  return tx.dex?.pending === true || (tx.ibc_transfers ?? []).some((t) => t.status === "pending" && !t.expired);
}

/**
 * A transaction: immutable once found and settled, `live` while unsettled
 * (`isUnsettled`). A 404 means "not indexed yet": the query is polled every
 * 4 s for the pending window, then given up on. Mount it under a `key` of
 * the hash so the window restarts per hash.
 */
export function useTransaction(hash: string) {
  const api = useApi();
  const [expired, setExpired] = useState(false);
  const query = useQuery({
    queryKey: queryKeys.transaction(hash),
    queryFn: ({ signal }) => api.transaction(hash.toUpperCase(), signal),
    ...immutable,
    refetchInterval: (q) => {
      if (q.state.data) return isUnsettled(q.state.data) ? live.refetchInterval : false;
      return isNotFound(q.state.error) && !expired ? TX_PENDING_POLL_MS : false;
    },
  });
  const error = useHeldNotFound(query.error, query.data !== undefined);
  const notFound = isNotFound(error);
  usePendingWindow(notFound, setExpired);

  const pending: PendingState = notFound ? (expired ? "not_found" : "looking") : null;
  return { ...query, error, pending };
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

/** The Hall of Fame members, the smallest first, and the rule the API applied (polled like the list). */
export function useHallOfFame() {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.hallOfFame(),
    queryFn: async ({ signal }): Promise<Pick<HallOfFame, "items" | "rules">> => {
      const { items, rules } = await api.hallOfFame(signal);
      return { items, rules };
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

/** Rows a numbered page of an account's activity holds. */
export const ACTIVITY_PER_PAGE = 25;

/**
 * Page `page` of an account's activity of one category (every one for null),
 * newest first, with the total: page 1 is live, an older page never changes
 * (new rows shift the pages, so the polled page 1's total tells how many came).
 * `limit` 1 reads only the total.
 */
export function useAccountActivity(
  address: string,
  { category, page, limit = ACTIVITY_PER_PAGE, enabled = true }: { category: ActivityCategory | null; page: number; limit?: number; enabled?: boolean },
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.accountActivity(address, category, page, limit),
    queryFn: ({ signal }) => api.accountActivity(address, { page, limit, category: category ?? undefined }, signal),
    enabled,
    ...(page === 1 ? live : immutable),
  });
}

/**
 * The account's latest 100 cross-chain transfers, both directions, polled:
 * only to decorate its activity rows with their status and the other chain
 * (an activity row never names the other chain).
 */
export function useAccountIbc(address: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.accountIbc(address),
    queryFn: ({ signal }) => api.accountIbc(address, { limit: 100 }, signal),
    select: (page) => page.items,
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

/** Several proposals at once (the upgrades the Future block page lists), each cached as `useProposal`'s. */
export function useProposalList(ids: number[]) {
  const api = useApi();
  return useQueries({
    queries: ids.map((id) => ({
      queryKey: queryKeys.proposal(id),
      queryFn: ({ signal }: { signal: AbortSignal }) => api.proposal(id, signal),
      ...live,
      refetchInterval: (q: { state: { data: Proposal | undefined } }) => (resolved(q.state.data) ? false : live.refetchInterval),
      staleTime: (q: { state: { data: Proposal | undefined } }) => (resolved(q.state.data) ? Infinity : live.staleTime),
    })),
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

/**
 * The recorded validators of a closed proposal's tally set that did not vote,
 * largest power first, 100 a page (a set rarely has more). Written once, so
 * read once; `enabled` false until the proposal says it has them.
 */
export function useProposalNonVoters(id: number, enabled = true) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.proposalNonVoters(id),
    queryFn: ({ pageParam, signal }) => api.proposalNonVoters(id, pageParam, signal),
    initialPageParam: firstPage(100),
    getNextPageParam: nextPage(100),
    enabled,
    ...immutable,
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

/**
 * Every module's live parameters: they change only through governance.
 * `poll` (the parameters page) refetches them every minute.
 */
export function useParams({ poll = false }: { poll?: boolean } = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.params(),
    queryFn: ({ signal }) => api.params(signal),
    ...(poll ? status : reference),
  });
}

/** The chain's module accounts by name, for moving between them on an account page. */
export function useModules() {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.modules(),
    queryFn: async ({ signal }) => (await api.modules(signal)).items,
    ...reference,
  });
}

/** The search route's matches; runs only when `enabled` and for a non-empty query, never polled. */
export function useSearch(q: string, enabled = true) {
  const api = useApi();
  const query = q.trim();
  return useQuery({
    queryKey: queryKeys.search(query),
    queryFn: ({ signal }) => api.search(query, signal),
    enabled: enabled && query !== "",
    ...reference,
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
