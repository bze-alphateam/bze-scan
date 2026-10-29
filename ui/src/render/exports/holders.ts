import BigNumber from "bignumber.js";

import type { ExplorerApi } from "@/api/client";
import type { Token, TokenHolder } from "@/api/types";
import { csvAmount, csvFileName } from "@/lib/csv";
import type { ExportDefinition, FilterValues } from "@/lib/export";
import { tokenSymbol } from "@/render/tokens";

/** The most holders one export keeps. */
export const HOLDERS_EXPORT_MAX = 20_000;
/** The route's largest page. */
const PAGE = 100;
/** Accounts the chain owns: module accounts and the IBC escrows (module-like). */
const MODULE_KINDS = new Set(["module", "ibc_escrow"]);

/** The minimum balance in base units, null when none is set, NaN when it is not a number. */
function minimumOf(filters: FilterValues, exponent: number): BigNumber | null {
  const text = String(filters.minBalance ?? "").trim();
  if (text === "") return null;
  const units = new BigNumber(text);
  return units.isFinite() && units.gte(0) ? units.shiftedBy(exponent) : new BigNumber(NaN);
}

/**
 * The export of a token's holders from the latest hourly snapshot, largest
 * balance first, up to 20,000. The route has no filters: the minimum ends the
 * read at the first smaller balance, module accounts are dropped as read.
 */
export function holdersExport({ api, token }: { api: ExplorerApi; token: Token }): ExportDefinition<TokenHolder, TokenHolder> {
  const symbol = tokenSymbol(token);
  return {
    title: `Export ${symbol} holders`,
    about:
      `You get a CSV file (comma separated) with the accounts holding ${symbol}, largest balance first, ` +
      "from the explorer's latest hourly count, so it can be up to an hour old. " +
      `Balances are in whole ${symbol}; the share is the percent of the whole supply each account holds. ` +
      "The rank is the account's place among all holders, module accounts included, even when you leave them out.",
    noun: "holders",
    fields: [
      { key: "rank", header: "Rank", value: (h) => h.rank },
      { key: "address", header: "Address", value: (h) => h.address },
      { key: "label", header: "Name", value: (h) => h.label?.name },
      { key: "balance", header: "Balance", value: (h) => csvAmount(h.balance, token.exponent) },
      { key: "symbol", header: "Symbol", value: () => symbol },
      { key: "share", header: "Share of supply (%)", value: (h) => h.share_pct },
    ],
    filters: [
      { kind: "amount", key: "minBalance", label: `Minimum balance (${symbol})`, placeholder: "No minimum" },
      {
        kind: "checkbox",
        key: "excludeModules",
        label: "Leave out module accounts",
        hint: "Accounts the chain owns, like the DEX, the burner or the IBC escrows.",
      },
    ],
    initialFilters: { minBalance: "", excludeModules: true },
    limit: { max: HOLDERS_EXPORT_MAX, initial: HOLDERS_EXPORT_MAX },
    validate: (filters) => (minimumOf(filters, token.exponent)?.isNaN() ? "The minimum balance must be a number, 0 or more." : null),
    source(filters) {
      const minimum = minimumOf(filters, token.exponent);
      return {
        fetchPage: async (cursor, signal) => {
          const res = await api.tokenHolders(token.denom, { cursor: cursor ?? undefined, limit: PAGE }, signal);
          return { items: res.items, next: res.next_cursor };
        },
        // Sorted by balance: the first one under the minimum ends the list.
        stop: minimum ? (h) => new BigNumber(h.balance).lt(minimum) : undefined,
        keep: filters.excludeModules ? (h) => !(h.label && MODULE_KINDS.has(h.label.kind)) : undefined,
        sizeHint: token.holders_count,
      };
    },
    lines: (h) => [h],
    fileName: (now) => csvFileName("holders", symbol, now),
  };
}
