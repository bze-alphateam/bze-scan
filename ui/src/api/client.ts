import type {
  Account,
  Activity,
  ActivityParams,
  Block,
  BlockListParams,
  ChainParams,
  BlockEvent,
  BlockSummary,
  BlockTransfer,
  ErrorBody,
  HallOfFame,
  IbcListParams,
  IbcTransfer,
  ModuleAccount,
  Page,
  PageParams,
  Proposal,
  ProposalDeposit,
  ProposalListParams,
  ProposalSummary,
  ProposalVote,
  RawRpcResponse,
  RawTx,
  SearchResponse,
  Stats,
  Status,
  Tx,
  TxListParams,
  Token,
  TokenEvent,
  TokenHolder,
  TokenListParams,
  TokenSummary,
  TokenTransfer,
  TxSummary,
  ValidatorDetail,
  ValidatorList,
  ValidatorListParams,
  VoteListParams,
} from "./types";

/**
 * The backend API, one method per route. Components reach it only through
 * `useApi()` (or the hooks built on it); the HTTP implementation is created
 * once, at the composition root, and tests inject a fake.
 */
export interface ExplorerApi {
  status(signal?: AbortSignal): Promise<Status>;

  blocks(params: BlockListParams, signal?: AbortSignal): Promise<Page<BlockSummary>>;
  block(height: number, signal?: AbortSignal): Promise<Block>;
  blockEvents(height: number, params: PageParams, signal?: AbortSignal): Promise<Page<BlockEvent>>;
  blockTransfers(height: number, params: PageParams, signal?: AbortSignal): Promise<Page<BlockTransfer>>;

  transactions(params: TxListParams, signal?: AbortSignal): Promise<Page<TxSummary>>;
  transaction(hash: string, signal?: AbortSignal): Promise<Tx>;

  validators(params: ValidatorListParams, signal?: AbortSignal): Promise<ValidatorList>;
  hallOfFame(signal?: AbortSignal): Promise<HallOfFame>;
  validator(operator: string, signal?: AbortSignal): Promise<ValidatorDetail>;
  validatorBlocks(operator: string, params: PageParams, signal?: AbortSignal): Promise<Page<BlockSummary>>;

  account(address: string, signal?: AbortSignal): Promise<Account>;
  accountActivity(address: string, params: ActivityParams, signal?: AbortSignal): Promise<Page<Activity>>;
  accountIbc(address: string, params: IbcListParams, signal?: AbortSignal): Promise<Page<IbcTransfer>>;

  tokens(params: TokenListParams, signal?: AbortSignal): Promise<Page<TokenSummary>>;
  token(denom: string, signal?: AbortSignal): Promise<Token>;
  tokenEvents(denom: string, params: PageParams, signal?: AbortSignal): Promise<Page<TokenEvent>>;
  tokenHolders(denom: string, params: PageParams, signal?: AbortSignal): Promise<Page<TokenHolder>>;
  tokenTransfers(denom: string, params: PageParams, signal?: AbortSignal): Promise<Page<TokenTransfer>>;

  proposals(params: ProposalListParams, signal?: AbortSignal): Promise<Page<ProposalSummary>>;
  proposal(id: number, signal?: AbortSignal): Promise<Proposal>;
  proposalVotes(id: number, params: VoteListParams, signal?: AbortSignal): Promise<Page<ProposalVote>>;
  proposalDeposits(id: number, params: PageParams, signal?: AbortSignal): Promise<Page<ProposalDeposit>>;

  search(q: string, signal?: AbortSignal): Promise<SearchResponse>;

  stats(signal?: AbortSignal): Promise<Stats>;
  params(signal?: AbortSignal): Promise<ChainParams>;
  modules(signal?: AbortSignal): Promise<{ items: ModuleAccount[] }>;

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

/** True for the API's 404: on a transaction, "not indexed yet". */
export function isNotFound(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
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

  /**
   * A denom route (`""`, `/events`, `/holders`, `/transfers`): the denom
   * URL-encoded on the path, and for a denom with `/` the `?denom=` form when
   * the path answers 404 (a proxy that decodes `%2F` breaks the path form).
   */
  async function byDenom<T>(denom: string, suffix: string, query: Query, signal?: AbortSignal): Promise<T> {
    try {
      return await get<T>(`/tokens/${seg(denom)}${suffix}`, query, signal);
    } catch (err) {
      if (!denom.includes("/") || !isNotFound(err)) throw err;
      return get<T>(`/token${suffix}`, { denom, ...query }, signal);
    }
  }

  return {
    status: (signal) => get("/status", {}, signal),

    blocks: (params, signal) => get("/blocks", { ...page(params), before: params.before }, signal),
    block: (height, signal) => get(`/blocks/${seg(String(height))}`, {}, signal),
    blockEvents: (height, params, signal) => get(`/blocks/${seg(String(height))}/events`, page(params), signal),
    blockTransfers: (height, params, signal) => get(`/blocks/${seg(String(height))}/transfers`, page(params), signal),

    transactions: (params, signal) => get("/txs", { ...page(params), status: params.status }, signal),
    transaction: (hash, signal) => get(`/txs/${seg(hash)}`, {}, signal),

    validators: (params, signal) => get("/validators", { ...page(params), status: params.status }, signal),
    hallOfFame: (signal) => get("/validators/hall-of-fame", {}, signal),
    validator: (operator, signal) => get(`/validators/${seg(operator)}`, {}, signal),
    validatorBlocks: (operator, params, signal) => get(`/validators/${seg(operator)}/blocks`, page(params), signal),

    account: (address, signal) => get(`/accounts/${seg(address)}`, {}, signal),
    accountActivity: (address, params, signal) =>
      get(`/accounts/${seg(address)}/activity`, { ...page(params), category: params.category }, signal),
    accountIbc: (address, params, signal) =>
      get(`/accounts/${seg(address)}/ibc`, { ...page(params), direction: params.direction }, signal),

    tokens: (params, signal) => get("/tokens", { ...page(params), kind: params.kind }, signal),
    token: (denom, signal) => byDenom(denom, "", {}, signal),
    tokenEvents: (denom, params, signal) => byDenom(denom, "/events", page(params), signal),
    tokenHolders: (denom, params, signal) => byDenom(denom, "/holders", page(params), signal),
    tokenTransfers: (denom, params, signal) => byDenom(denom, "/transfers", page(params), signal),

    proposals: (params, signal) => get("/proposals", { ...page(params), status: params.status }, signal),
    proposal: (id, signal) => get(`/proposals/${seg(String(id))}`, {}, signal),
    proposalVotes: (id, params, signal) =>
      get(`/proposals/${seg(String(id))}/votes`, { ...page(params), option: params.option, voter: params.voter }, signal),
    proposalDeposits: (id, params, signal) => get(`/proposals/${seg(String(id))}/deposits`, page(params), signal),

    search: (q, signal) => get("/search", { q }, signal),

    stats: (signal) => get("/stats", {}, signal),
    params: (signal) => get("/params", {}, signal),
    modules: (signal) => get("/modules", {}, signal),

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
