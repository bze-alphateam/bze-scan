import type { ExplorerApi } from "@/api/client";
import type { Activity, ActivityCategory, Balance, TokenSummary } from "@/api/types";
import { csvAmount, csvFileName, csvTime } from "@/lib/csv";
import type { ExportDefinition } from "@/lib/export";
import { ACTIVITY_FILTERS, activityLine, lineText, withoutFee } from "@/render/activity";
import { createRenderContext } from "@/render/messages/registry";
import type { RenderContext } from "@/render/messages/types";

/** The most transactions one export reads. */
export const ACTIVITY_EXPORT_MAX = 10_000;
/** The route's largest page. */
const PAGE = 100;

/** One CSV line: a row of the account's activity and one denom it moved (null when it moved none). */
export interface ActivityCsvLine {
  row: Activity;
  /** The row's first line: the one carrying the fee. */
  first: boolean;
  amount: string | null;
  symbol: string | null;
  denom: string | null;
  description: string;
  fee: { amount: string; symbol: string } | null;
}

/** A balance in display units with the symbol to name it; the raw amount and no symbol when the exponent is unknown. */
function display(b: Balance, ctx: RenderContext): { amount: string; symbol: string | null } {
  const info = b.symbol != null && b.exponent != null ? { symbol: b.symbol, exponent: b.exponent } : ctx.denom(b.denom);
  if (info.exponent == null) return { amount: csvAmount(b.amount, 0), symbol: null };
  return { amount: csvAmount(b.amount, info.exponent), symbol: info.symbol };
}

/** The fee, every denom of it joined with "; " (one denom in practice). */
function feeOf(row: Activity, ctx: RenderContext): ActivityCsvLine["fee"] {
  const paid = (row.fee ?? []).filter((f) => f.amount !== "0");
  if (paid.length === 0) return null;
  const shown = paid.map((f) => display(f, ctx));
  return {
    amount: shown.map((f) => f.amount).join("; "),
    symbol: shown.map((f, i) => f.symbol ?? paid[i].denom).join("; "),
  };
}

/** A row's CSV lines: one per denom the action moved (the fee apart), or one with no amount. */
export function activityLines(row: Activity, address: string, ctx: RenderContext): ActivityCsvLine[] {
  const description = lineText(activityLine(row, { address, ctx }));
  const fee = feeOf(row, ctx);
  const moved = withoutFee(row);
  if (moved.length === 0) {
    return [{ row, first: true, amount: null, symbol: null, denom: null, description, fee }];
  }
  return moved.map((b, i) => ({ row, first: i === 0, ...display(b, ctx), denom: b.denom, description, fee: i === 0 ? fee : null }));
}

/**
 * The export of an account's activity: every row of a category, newest
 * first, up to 10,000, as one line per denom moved.
 */
export function activityExport({
  api,
  address,
  category,
  tokens,
}: {
  api: ExplorerApi;
  address: string;
  category: ActivityCategory | null;
  tokens?: Map<string, TokenSummary>;
}): ExportDefinition<Activity, ActivityCsvLine> {
  const ctx = createRenderContext({ transfers: [], tokens });
  return {
    title: "Export transactions",
    about:
      "You get a CSV file (comma separated) with this account's transactions, newest first. " +
      "Each line is one token a transaction moved, so a trade that sold one token and bought another takes two lines. " +
      "Amounts are in whole tokens, signed: minus for what left the account, plus for what came in; the fee is apart, on the transaction's first line. " +
      "Times are in UTC. Rows without a transaction hash are things the chain did for the account at the end of a block, like a DEX order getting filled.",
    noun: "transactions",
    fields: [
      { key: "time", header: "Time (UTC)", value: (l) => csvTime(l.row.time) },
      { key: "height", header: "Block", value: (l) => l.row.height },
      { key: "tx_hash", header: "Transaction hash", value: (l) => l.row.tx_hash },
      { key: "description", header: "Description", value: (l) => l.description },
      { key: "category", header: "Category", value: (l) => l.row.category },
      { key: "kind", header: "Kind", value: (l) => l.row.kind },
      { key: "amount", header: "Amount", value: (l) => l.amount },
      { key: "symbol", header: "Symbol", value: (l) => l.symbol },
      { key: "denom", header: "Denom", value: (l) => l.denom, defaultOn: false },
      { key: "fee", header: "Fee", value: (l) => l.fee?.amount },
      { key: "fee_symbol", header: "Fee symbol", value: (l) => l.fee?.symbol },
      { key: "counterparty", header: "Counterparty", value: (l) => l.row.counterparty?.address },
      {
        key: "counterparty_name",
        header: "Counterparty name",
        value: (l) => l.row.counterparty?.label?.name ?? l.row.counterparty?.moniker,
      },
      { key: "success", header: "Success", value: (l) => l.row.success },
      { key: "signer", header: "Signed by this account", value: (l) => l.row.is_signer, defaultOn: false },
      { key: "msg_types", header: "Message types", value: (l) => l.row.msg_types.join("; "), defaultOn: false },
    ],
    filters: [
      {
        kind: "select",
        key: "category",
        label: "Category",
        options: ACTIVITY_FILTERS.map((f) => ({ label: f.label, value: f.value ?? "" })),
      },
      {
        kind: "checkbox",
        key: "blockLevel",
        label: "Include rows without a transaction",
        hint: "What the chain did for the account at the end of a block: DEX orders filled, unbondings finished, rewards and raffles won.",
      },
    ],
    initialFilters: { category: category ?? "", blockLevel: true },
    limit: { max: ACTIVITY_EXPORT_MAX, initial: ACTIVITY_EXPORT_MAX },
    source(filters) {
      const chosen = (filters.category as ActivityCategory | "") || undefined;
      return {
        // The first read is a numbered page: it carries the total for the progress bar.
        fetchPage: async (cursor, signal) => {
          const res = await api.accountActivity(
            address,
            cursor == null ? { page: 1, limit: PAGE, category: chosen } : { cursor, limit: PAGE, category: chosen },
            signal,
          );
          return { items: res.items, next: res.next_cursor, total: res.total ?? null };
        },
        keep: filters.blockLevel ? undefined : (row) => row.tx_hash != null,
      };
    },
    lines: (row) => activityLines(row, address, ctx),
    fileName: (now) => csvFileName("transactions", address, now),
  };
}
