import type { CsvValue } from "./csv";
import type { RateLimiter } from "./rate-limit";

// The CSV export of a list (BFE-279): a definition says what the list's
// export offers (fields, filters, limit) and how to read it; `runExport`
// pages through it under a rate limiter; the modal builds the file.

/** A CSV column: its header and how a line fills it. */
export interface ExportField<L> {
  key: string;
  header: string;
  value: (line: L) => CsvValue;
  /** Ticked when the modal opens; true unless set. */
  defaultOn?: boolean;
}

/** A filter the modal shows over the fields. */
export type ExportFilter =
  | { kind: "select"; key: string; label: string; options: { label: string; value: string }[] }
  | { kind: "checkbox"; key: string; label: string; hint?: string }
  | { kind: "amount"; key: string; label: string; hint?: string; placeholder?: string };

export type FilterValues = Record<string, string | boolean>;

/** One read of the list: the items and the cursor of the next page (null on the last), `total` when the route counts. */
export interface ExportPage<T> {
  items: T[];
  next: string | null;
  total?: number | null;
}

/** How to read the list under the chosen filters. */
export interface ExportSource<T> {
  fetchPage(cursor: string | null, signal: AbortSignal): Promise<ExportPage<T>>;
  /** False drops the item from the file; it still counts as read. */
  keep?(item: T): boolean;
  /** True ends the export at this item, which is left out (the rest of the list is filtered out too). */
  stop?(item: T): boolean;
  /** The size of the list before `keep`, when known without a read: the progress bar's guess. */
  sizeHint?: number | null;
}

export interface ExportDefinition<T, L> {
  /** The modal's title: "Export transactions". */
  title: string;
  /** The modal's info box: what the downloaded file holds, in plain words. */
  about: string;
  /** What the limit counts: "transactions", "holders". */
  noun: string;
  fields: ExportField<L>[];
  filters: ExportFilter[];
  initialFilters: FilterValues;
  limit: { max: number; initial: number };
  /** A message when the filters cannot be used, else null. */
  validate?(filters: FilterValues): string | null;
  source(filters: FilterValues): ExportSource<T>;
  /** The CSV lines of an item: one, or one per denom it moved. */
  lines(item: T): L[];
  fileName(now: Date): string;
}

/** Where an export stands: what it kept, how far it read, and where the next read starts. */
export interface ExportProgress<T> {
  items: T[];
  /** Items read, kept or not. */
  scanned: number;
  cursor: string | null;
  /** The route's own count of the list, once a read gave one. */
  total: number | null;
  finished: boolean;
}

export const startProgress = <T>(): ExportProgress<T> => ({ items: [], scanned: 0, cursor: null, total: null, finished: false });

/**
 * The share of the work done, 0 to 1: kept items against the limit, or read
 * items against the list's size when that is what ends it first. Never 1
 * before the export finished.
 */
export function progressShare(p: ExportProgress<unknown>, limit: number, sizeHint: number | null = null): number {
  if (p.finished) return 1;
  const size = p.total ?? sizeHint;
  const share = Math.max(p.items.length / limit, size ? p.scanned / size : 0);
  return Math.min(share, 0.99);
}

/**
 * Reads the list page by page, each read waiting for the limiter, until
 * `limit` items are kept, the list ends, or `stop` says the rest is filtered
 * out. Starts from `from` (a retry carries on where a failure left off) and
 * reports every page to `onProgress`; a failed read rejects, and the last
 * reported progress is where to retry from.
 */
export async function runExport<T>(
  source: ExportSource<T>,
  {
    limit,
    limiter,
    signal,
    from = startProgress<T>(),
    onProgress,
  }: {
    limit: number;
    limiter: RateLimiter;
    signal: AbortSignal;
    from?: ExportProgress<T>;
    onProgress?: (p: ExportProgress<T>) => void;
  },
): Promise<ExportProgress<T>> {
  let p = from;
  while (!p.finished) {
    await limiter.wait(signal);
    const page = await source.fetchPage(p.cursor, signal);
    const items = [...p.items];
    let scanned = p.scanned;
    let stopped = false;
    for (const item of page.items) {
      if (source.stop?.(item)) {
        stopped = true;
        break;
      }
      scanned++;
      if (source.keep && !source.keep(item)) continue;
      items.push(item);
      if (items.length >= limit) break;
    }
    // A cursor that does not move would read the same page forever.
    const ended = page.next == null || page.next === p.cursor || page.items.length === 0;
    p = {
      items,
      scanned,
      cursor: page.next,
      total: page.total ?? p.total,
      finished: stopped || ended || items.length >= limit,
    };
    onProgress?.(p);
  }
  return p;
}
