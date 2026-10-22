import { serverApiUrl } from "./env";

/** How long the Next.js server waits for the API before it renders without the answer. */
export const SERVER_FETCH_TIMEOUT_MS = 5000;

/** The API's answer as the server saw it: the parsed body, or the HTTP status (null when the request itself failed). */
export type ServerJson<T> = { ok: true; data: T } | { ok: false; status: number | null };

/**
 * A GET of `path` (e.g. `/v1/txs/…`) on the API, from the Next.js server.
 *
 * The body is always read or cancelled: an unread body keeps its connection
 * and buffers alive until garbage collection, and every page view of an
 * unknown account or a pending transaction gets one.
 */
export async function serverJson<T>(path: string, fetchFn: typeof fetch = fetch): Promise<ServerJson<T>> {
  let res: Response;
  try {
    res = await fetchFn(`${serverApiUrl()}${path}`, {
      cache: "no-store",
      headers: { Accept: "application/json" },
      signal: AbortSignal.timeout(SERVER_FETCH_TIMEOUT_MS),
    });
  } catch {
    return { ok: false, status: null };
  }
  if (!res.ok) {
    await res.body?.cancel().catch(() => undefined);
    return { ok: false, status: res.status };
  }
  try {
    return { ok: true, data: (await res.json()) as T };
  } catch {
    return { ok: false, status: null };
  }
}
