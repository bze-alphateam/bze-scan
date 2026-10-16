import { QueryClient } from "@tanstack/react-query";

import { ApiError } from "./client";

// The three refetch policies every query picks one of (UI Design, "Data").
// Page code chooses a policy; it never sets these numbers itself.

const SECOND = 1000;

/** Home, lists (first page), accounts, validators…: refetched every 4 s, paused in background tabs. */
export const live = {
  staleTime: 4 * SECOND,
  refetchInterval: 4 * SECOND,
  refetchIntervalInBackground: false,
} as const;

/** A block by height, a found transaction, older list pages, raw JSON: fetched once, kept an hour. */
export const immutable = {
  staleTime: Infinity,
  gcTime: 60 * 60 * SECOND,
  refetchInterval: false,
  refetchOnWindowFocus: false,
  refetchOnReconnect: false,
} as const;

/** The status light: refetched every 60 s. */
export const status = {
  staleTime: 60 * SECOND,
  refetchInterval: 60 * SECOND,
  refetchIntervalInBackground: false,
} as const;

/**
 * Lookup data a page decorates with (validator monikers, token prices):
 * fresh for a minute, never polled.
 */
export const reference = {
  staleTime: 60 * SECOND,
  refetchInterval: false,
} as const;

/** How often a transaction the API does not know yet (404, "pending") is asked for again. */
export const TX_PENDING_POLL_MS = 4 * SECOND;

/**
 * How long a pending transaction is looked for before the page says it is not
 * found: two minutes, or `NEXT_PUBLIC_TX_PENDING_WINDOW_MS` (the e2e build
 * shortens it).
 */
export const TX_PENDING_WINDOW_MS = Number(process.env.NEXT_PUBLIC_TX_PENDING_WINDOW_MS) || 2 * 60 * SECOND;

/** Never retries a 4xx (the same request gets the same answer); twice otherwise. */
export function shouldRetry(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError && error.isClientError) {
    return false;
  }
  return failureCount < 2;
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: shouldRetry },
    },
  });
}
