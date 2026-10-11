import type {
  Account,
  Block,
  BlockEvent,
  BlockSummary,
  ErrorBody,
  Page,
  PageParams,
  RawRpcResponse,
  RawTx,
  SearchResponse,
  Status,
  Tx,
  TxListParams,
  TxSummary,
  ValidatorDetail,
  ValidatorList,
  ValidatorListParams,
} from "./types";

/**
 * The backend API, one method per route. Components reach it only through
 * `useApi()` (or the hooks built on it); the HTTP implementation is created
 * once, at the composition root, and tests inject a fake.
 */
export interface ExplorerApi {
  status(signal?: AbortSignal): Promise<Status>;

  blocks(params: PageParams, signal?: AbortSignal): Promise<Page<BlockSummary>>;
  block(height: number, signal?: AbortSignal): Promise<Block>;
  blockEvents(height: number, params: PageParams, signal?: AbortSignal): Promise<Page<BlockEvent>>;

  transactions(params: TxListParams, signal?: AbortSignal): Promise<Page<TxSummary>>;
  transaction(hash: string, signal?: AbortSignal): Promise<Tx>;

  validators(params: ValidatorListParams, signal?: AbortSignal): Promise<ValidatorList>;
  validator(operator: string, signal?: AbortSignal): Promise<ValidatorDetail>;
  validatorBlocks(operator: string, params: PageParams, signal?: AbortSignal): Promise<Page<BlockSummary>>;

  account(address: string, signal?: AbortSignal): Promise<Account>;

  search(q: string, signal?: AbortSignal): Promise<SearchResponse>;

  rawBlock(height: number, signal?: AbortSignal): Promise<RawRpcResponse>;
  rawBlockResults(height: number, signal?: AbortSignal): Promise<RawRpcResponse>;
  rawCommit(height: number, signal?: AbortSignal): Promise<RawRpcResponse>;
  rawTx(hash: string, signal?: AbortSignal): Promise<RawTx>;
}

/** A failed request: the HTTP status and the error envelope's code and message. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }

  /** True for 4xx answers: retrying them returns the same answer. */
  get isClientError(): boolean {
    return this.status >= 400 && this.status < 500;
  }
}

export interface HttpApiOptions {
  /** The API's base URL without a trailing slash, e.g. `https://scan.getbze.com/api`. */
  baseUrl: string;
  fetch: typeof globalThis.fetch;
}

type Query = Record<string, string | number | undefined>;

/** The `ExplorerApi` over HTTP: `${baseUrl}/v1/...`, path values encoded. */
export function createHttpApi({ baseUrl, fetch }: HttpApiOptions): ExplorerApi {
  const root = baseUrl.replace(/\/+$/, "");

  async function get<T>(path: string, query: Query = {}, signal?: AbortSignal): Promise<T> {
    const search = new URLSearchParams();
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined && value !== "") {
        search.set(key, String(value));
      }
    }
    const qs = search.toString();
    const res = await fetch(`${root}/v1${path}${qs ? `?${qs}` : ""}`, {
      headers: { Accept: "application/json" },
      signal,
    });
    if (!res.ok) {
      throw await toApiError(res);
    }
    return (await res.json()) as T;
  }

  const seg = encodeURIComponent;
  const page = (p: PageParams): Query => ({ cursor: p.cursor, limit: p.limit });

  return {
    status: (signal) => get("/status", {}, signal),

    blocks: (params, signal) => get("/blocks", page(params), signal),
    block: (height, signal) => get(`/blocks/${seg(String(height))}`, {}, signal),
    blockEvents: (height, params, signal) => get(`/blocks/${seg(String(height))}/events`, page(params), signal),

    transactions: (params, signal) => get("/txs", { ...page(params), status: params.status }, signal),
    transaction: (hash, signal) => get(`/txs/${seg(hash)}`, {}, signal),

    validators: (params, signal) => get("/validators", { ...page(params), status: params.status }, signal),
    validator: (operator, signal) => get(`/validators/${seg(operator)}`, {}, signal),
    validatorBlocks: (operator, params, signal) => get(`/validators/${seg(operator)}/blocks`, page(params), signal),

    account: (address, signal) => get(`/accounts/${seg(address)}`, {}, signal),

    search: (q, signal) => get("/search", { q }, signal),

    rawBlock: (height, signal) => get(`/raw/block/${seg(String(height))}`, {}, signal),
    rawBlockResults: (height, signal) => get(`/raw/block_results/${seg(String(height))}`, {}, signal),
    rawCommit: (height, signal) => get(`/raw/commit/${seg(String(height))}`, {}, signal),
    rawTx: (hash, signal) => get(`/raw/tx/${seg(hash)}`, {}, signal),
  };
}

async function toApiError(res: Response): Promise<ApiError> {
  try {
    const body = (await res.json()) as Partial<ErrorBody>;
    if (body.error && typeof body.error.code === "string") {
      return new ApiError(res.status, body.error.code, body.error.message ?? "");
    }
  } catch {
    // Not JSON (a proxy's error page): fall through.
  }
  return new ApiError(res.status, "http_error", res.statusText || `HTTP ${res.status}`);
}
