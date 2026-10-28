# bze-scan backend

Go module `github.com/bze-alphateam/bze-scan/backend`, Go 1.26 (the floor is set
by the `bze` chain module it imports to decode message types).

One binary, `bze-scan`, with subcommands:

- `serve` — the production process: the read-only HTTP API with its raw-JSON
  proxy (archive responses cached in memory) and its status endpoint, the
  live indexer (woken by a PostgreSQL notification at each block the node's
  `psql` indexer commits, with a height cursor as the guarantee), the state
  sync (driven by what the indexer sees, with tickers as a safety net) and,
  when enabled by configuration, the backfill. One process, one log.
- `migrate` — `up` (the default), `down <n>` and `version` over the
  versioned migrations: the explorer schema, its partitions, the one
  notification trigger on the CometBFT `psql` indexer's `blocks` table, and
  the classification tables. The indexer's own tables are never altered
  otherwise.
- `backfill` — walks history backwards from archive nodes through a pool of
  workers and one batching writer, at a polite rate, normalising old event
  formats. Checkpointed and resumable; never writes the node's indexer tables.
- `reindex` — re-runs a list of heights or a range through the same pipeline,
  from the local node while it still has them and from archive nodes otherwise.
- `sync-state` — one full refresh of validators, proposals, denominations,
  holders, labels, the Cosmos chain registry cache, token prices, the
  parameter snapshots, the open orders' reconciliation, the proposals'
  non-voters, the upgrade recovery, the chain state and the daily statistics, for operations (it
  reads the node's gRPC and its `/status`); `--job <set>` runs one set,
  `--job <set> --id <key>` one entry of it, `--job daily_stats --from
  YYYY-MM-DD --to YYYY-MM-DD` recomputes those days from the database alone.
- `snapshots` — reads past state from the archive node at the height it
  describes (the bonded tokens, votes and deposits of resolved proposals)
  and writes it over the database's; run by hand only (see Snapshots below).
- `version` — prints the commit the binary was built from (`dev` for a plain
  `go build`; the image sets it with `-ldflags "-X main.version=<sha>"`).

Every node call is by height (`/block`, `/block_results`, `/commit`,
`/validators`, `/status`, `/abci_query` at a height and gRPC state queries); the search routes are never
used, on any node. The one live read is the halt watch's `/consensus_state`
on the local node, only while the chain is halted for an upgrade.

The raw-JSON routes serve a block, its results, its commit and a transaction
(sliced from its block by index) from an in-memory cache that the live indexer
fills at index time and that is filled from the archive nodes on a miss. The
local node is never asked for them, and nothing raw is written to the database.

The status endpoint returns a snapshot that a checker refreshes once a minute,
so a request never touches a node or the database. `live_fill` is healthy when
the last indexed height moved since the previous check and is within 5 heights
of both the local node and an archive node; it carries the three heights.
`back_fill` says whether the backfill is finished or in progress and the
oldest indexed height. The response is HTTP 200 whenever the process serves;
the JSON carries the verdict, so a halted chain never blocks a deploy.

Implemented so far:

- `serve` with the HTTP server and `GET /health` (200, empty body, whenever
  the process serves HTTP; it checks neither the database nor a node).
  Unknown paths answer 404 with the JSON error envelope
  `{"error":{"code":"not_found","message":"Not Found"}}`.
- The read API under `/api/v1`: blocks, transactions, validators, accounts
  and search (see HTTP API below).
- `GET /api/v1/status` with its checker (see HTTP API below).
- The raw-JSON routes under `/api/v1/raw` with their in-memory cache (see
  HTTP API below).
- `migrate` (see Migrations below).
- The live indexer inside `serve`, writing `explorer.blocks`, transactions,
  messages, validator events and accounts, with the catch-up through the archive after a long outage
  (see Live indexer below).
- `backfill`, standalone or inside `serve` (see Backfill below).
- `reindex` (see Reindex below).
- The state sync inside `serve` and the `sync-state` command, with the
  validators, denoms, proposals, holders, prices, params and chain-state
  sets, `GET /api/v1/validators`, the token, proposal, stats and parameters
  routes (see State sync below).

## Configuration

Environment variables; a `.env` in the working directory is loaded when
present (variables already set in the environment win). `.env.dist` is the
documented template with the defaults:

| Variable | Default | Meaning |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | listen address of the HTTP API |
| `CORS_ALLOWED_ORIGINS` | empty | comma-separated origins allowed to call the API from a browser, or `*`; empty sends no CORS headers |
| `LOG_LEVEL` | `info` | logrus level |
| `LOG_FORMAT` | `text` | `text` or `json` |
| `DATABASE_URL` | none | PostgreSQL URL of the node's database; required by `migrate` and `serve` (the API reads it, with or without the indexer). Never logged |
| `NODE_RPC_URL` | `http://127.0.0.1:26657` | CometBFT RPC of the local node, read by height only |
| `CHAIN_ID` | `beezee-1` | `serve` refuses to start the indexer when the node's `/status` reports another network |
| `INDEXER_ENABLED` | `true` | runs the live indexer and the state sync in `serve`; `false` runs the HTTP API only: the one way to run a second process against the same database |
| `NODE_GRPC_ADDR` | `127.0.0.1:9090` | `host:port` of the local node's gRPC server, which the state sync and the account route's live reads query (the API on a connection of its own, 3 s per call); never a public node |
| `NODE_GRPC_TLS` | `false` | dial `NODE_GRPC_ADDR` with TLS |
| `ARCHIVE_RPC_URL` | `https://rpc.getbze.com` | CometBFT RPC of an archive node, by height only; the status checker compares its tip, the raw-JSON routes fetch their misses from it, the backfill and the catch-up read history from it |
| `ARCHIVE_RPC_RETRY_URL` | empty | tried when `ARCHIVE_RPC_URL` fails; empty means `ARCHIVE_RPC_URL` again |
| `STATUS_INTERVAL` | `60s` | period of the status checker's ticks (a Go duration) |
| `STATUS_HEIGHT_TOLERANCE` | `5` | largest spread, in blocks, between the explorer, the local node and the archive that is still healthy |
| `RAW_CACHE_MAX_ENTRIES` | `300` | entries kept per raw-JSON route (one per height) |
| `RAW_CACHE_TTL` | `20m` | how long a raw-JSON entry lives after it was stored (a Go duration) |
| `BACKFILL_ENABLED` | `false` | run the main backfill job inside `serve`; when `false`, an unfinished job is marked `paused`. The `backfill` command ignores it |
| `BACKFILL_FLOOR` | `genesis` | lowest height the backfill indexes: `genesis`, a height, or a `YYYY-MM-DD` date (the first block at or after that UTC midnight) |
| `BACKFILL_WORKERS` | `10` | heights fetched in parallel (X) |
| `BACKFILL_BATCH` | `50` | heights per flush (M), one transaction each |
| `BACKFILL_QUIET` | `2s` | flush what the writer holds after this long without a new height |
| `BACKFILL_RATE_LIMIT` | `20` | archive requests per second across every worker of the backfill and the catch-up (three per height) |
| `COINGECKO_HOST` | `https://api.coingecko.com` | CoinGecko's API host, whose USD prices back the aggregator-compatible `/api/prices` and the prices job |
| `COINGECKO_PRICE_IDS` | `bzedge` | comma-separated CoinGecko ids priced, in the order `/api/prices` lists them; mainnet uses `bzedge,osmosis,cosmos` |
| `MEDIUM_FEED_URL` | `https://medium.com/feed/bzedge-community` | Medium RSS feed whose first six articles `/api/articles/medium` serves |
| `AGGREGATOR_URL` | empty | base URL of the BZE aggregator (production `https://getbze.com`); its `/api/dex/tickers` gives BZE's 24-hour change; empty leaves the change alone |
| `PRICE_CHANGE_MARKET` | mainnet's BZE/USDC.inj pool, `ibc/81DA…4838_ubze` | the aggregator market whose ticker gives BZE's 24-hour change; change it when the USDC pool moves again; empty leaves the change null |
| `CHAIN_REGISTRY_API_URL` | `https://api.github.com/repos/cosmos/chain-registry/contents` | GitHub contents API of the Cosmos chain registry, listed by the chain_registry job |
| `CHAIN_REGISTRY_RAW_URL` | `https://raw.githubusercontent.com/cosmos/chain-registry/master` | base of the registry's raw `chain.json` / `assetlist.json` files |
| `KEYBASE_URL` | `https://keybase.io/_/api/1.0/user/lookup.json` | the Keybase user lookup the validators set reads validator logos from (`?key_suffix=<identity>&fields=pictures`), at most once a day per validator, 10 s per call |
| `HALT_WATCH_ENABLED` | `true` | with the indexer: run the halt watch (see State sync, Upgrade recovery) |
| `HALT_WATCH_INTERVAL` | `5s` | how often the halt watch polls the local node during an upgrade's halt |
| `UPGRADE_RECOVERY_WINDOW` | `10000` | heights after an upgrade whose commits the `upgrade_recovery` set reads (at `BACKFILL_RATE_LIMIT`) for the validators not back yet |
| `SINK_RETENTION` | empty | how long the CometBFT psql sink keeps a block (a Go duration, e.g. `48h`); set, `serve` runs the sink cleanup (see Sink cleanup). Empty turns it off: keep it off while another reader of the sink (the aggregator's listener) still needs the rows |
| `SINK_CLEANUP_INTERVAL` | `24h` | period of the sink cleanup inside `serve` |
| `SINK_CLEANUP_BATCH` | `1000` | heights the sink cleanup deletes per transaction |
| `DEBUG_ADDR` | empty | `host:port` on which `serve` exposes `net/http/pprof` under `/debug/pprof/`, on a server of its own; keep it on localhost (`127.0.0.1:6060`). Empty exposes nothing |

Invalid values stop the process at startup with every problem listed.

## Commands

`make` (or `make help`) lists the targets:

| Target | What it does |
| --- | --- |
| `make build` | builds `build/bze-scan` |
| `make run` | runs `bze-scan serve` |
| `make migrate` | runs `bze-scan migrate up` against the compose database (or `DATABASE_URL` when set) |
| `bze-scan backfill` | runs the main backfill job standalone with the same configuration (see Backfill below); exit 0 once the floor is reached, 1 otherwise |
| `bze-scan reindex` | re-indexes heights and overwrites their rows (see Reindex below); exit 0 when every height succeeded, 2 when some failed, 1 on a configuration or database error |
| `bze-scan sync-state [--job <set> [--id <key>]...] [--from YYYY-MM-DD --to YYYY-MM-DD]` | one full resync of every state-sync set, or of `--job` alone (see State sync below); exit 0 when every set succeeded, 1 when any failed. `--id` resyncs only those entries of the set (`--job proposal_non_voters --id 47` computes proposal 47's non-voters again, `--job upgrade_recovery --id 22551810` reads the commits after the upgrade planned at that height again). `--job daily_stats --from --to` recomputes those closed UTC days of `daily_stats` (after a reindex, for example) |
| `bze-scan snapshots [--kind proposals] [--id N]... [--workers N]` | reads past state from the archive and overwrites the database's (see Snapshots below); exit 0 when every target succeeded, 2 when some failed (listed on stderr), 1 on a configuration or database error |
| `bze-scan sink-cleanup [--retention 48h] [--dry-run]` | one pass of the sink cleanup (see Sink cleanup below); `--dry-run` prints the cut-off and the rows per table and deletes nothing; refuses to run without `--retention` or `SINK_RETENTION`; exit 0 on success, 1 on a configuration or database error |
| `bze-scan version` | prints the build commit |
| `make check` | everything CI runs, in CI's order |
| `make test` | unit tests, `go test ./... -race` (no network, no docker) |
| `make vet` | `go vet -tags=e2e ./...` |
| `make lint` | `golangci-lint run` (linters and formatters, acceptance tests included) |
| `make tidy-check` | fails when `go.mod`/`go.sum` are not tidy |
| `make vulncheck` | `govulncheck`; fails on a reachable vulnerability that has a fixed version, lists the ones without a fix |
| `make e2e` | starts PostgreSQL from `../docker/compose.yml`, runs the acceptance tests in `e2e/` (build tag `e2e`, `-race`), tears the database down; the exit code is the tests' |
| `make e2e-up` / `make e2e-down` | starts / removes that PostgreSQL by hand |
| `make fixtures HEIGHTS="..."` | records node fixtures for the fake node (see below) |
| `make grpc-fixtures VALIDATORS="..." ACCOUNTS="..."` | records the fake gRPC server's fixtures (see below) |
| `make registry-fixtures [CHAINS="..."]` | records the fake chain registry's files (see below) |
| `make soak-db-up` / `make soak-db-down` | starts / stops the soak's PostgreSQL (`../docker/compose.soak.yml`: a named volume instead of tmpfs, project `bze-scan-soak`) |
| `make node-build` / `make node-init` | builds `bzed` from source at the tag mainnet runs into `.local/bin` / writes the state-synced mainnet home `.local/bzed-home` with the `psql` sink into `DATABASE_URL` (localhost only) |
| `SOAK=1 make node-start` / `make node-reset` | runs that node in the foreground (refuses without `SOAK=1`) / deletes its home |
| `make clean` | removes `build/` |

The `node-*` and `soak-db-*` targets exist for the mainnet soak only; the
runbook is [docs/soak.md](docs/soak.md).

The acceptance tests read `E2E_DATABASE_URL`, defaulting to the compose
database `postgres://bze:bze@127.0.0.1:15432/bze_index?sslmode=disable`.
CI runs four independent workflows on every pull request touching
`backend/` (and, for e2e and the image, `docker/`), each its own check:

| Workflow | Runs |
| --- | --- |
| `backend-lint.yml` (Backend lint) | build, `make vet`, golangci-lint, `make tidy-check`, `make vulncheck` |
| `backend-unit.yml` (Backend unit tests) | `make test` |
| `backend-e2e.yml` (Backend e2e tests) | `make e2e` |
| `backend-image.yml` (Backend image) | builds `docker/backend.Dockerfile` and runs `docker/smoke-test.sh` on it |

On pushes to `main`, `backend-image.yml` calls the unit and e2e workflows
itself and pushes the image to GHCR only when both pass (see
`docker/README.md`); lint still runs on its own. `make check` runs all of it
locally except the image.

## Layout

```
cmd/bze-scan/      main: runs app/cli
app/cli/           cobra root and subcommands (serve, migrate, backfill,
                   reindex, sync-state, snapshots) and their exit codes
config/            environment parsing and validation
migrations/        SQL migrations (embedded, up and down), the Migrator,
                   post-migration steps, partition math
app/serve/         the serve process: its components in one errgroup; the
                   standalone backfill and the reindex job
app/server/        echo wiring and the graceful HTTP runner
app/controller/    thin HTTP handlers: parsing, validation, status codes
app/dto/           JSON shapes of the API and the keyset cursor
app/repository/    read queries over the explorer tables (pgx)
app/middleware/    request id, request log, panic recovery, JSON error
                   handler
internal/archive/  normalises the pre-v8 event format of archive heights
                   (generation table, legacy names), archive path only
internal/chain/    chain facts (bech32 prefix, module account addresses), the
                   chain's codec (transaction decoding, pre-v8 type URLs),
                   coin strings
internal/classify/ message and block-event classification (Go source of
                   truth, mirrored into SQL by migrate)
internal/node/     CometBFT RPC client, by-height routes only (abci_query at
                   a height included)
internal/route/    the local node for the heights it still has, else the
                   archive (reindex, active set)
internal/activeset/
                   the CometBFT validator set at a height, with operators
internal/statesync/upgrades/
                   upgrade recovery: the halt watch and the
                   upgrade_recovery set
internal/snapshots/
                   the snapshots command: the archive reader (paced), the
                   Kind interface and the runner; kinds in subpackages
                   (proposals)
internal/sink/     the sink cleanup: cut-off, batched children-first
                   deletes of the CometBFT psql sink tables
internal/transform/
                   node answers of one height -> explorer rows (no I/O)
internal/writer/   transactional, idempotent writes (the live writer, the
                   batch writer of the backfill and the reindex; insert and
                   update modes)
internal/indexer/live/
                   LISTEN/NOTIFY listener, height cursor, retries
internal/indexer/backfill/
                   the fetch-parallel, write-serial pipeline, the main job
                   (checkpoint, advisory lock) and the catch-up
internal/indexer/reindex/
                   selectors, local-or-archive routing by height and the
                   reindex run over the backfill pipeline in update mode
internal/grpcclient/
                   the local node's gRPC connection (chain codec, 10 s per
                   call)
internal/chainstate/
                   live account reads (balances, delegations, unbonding,
                   rewards) behind a 5 s in-memory cache
internal/ordercoverage/
                   whether explorer.orders is complete for open orders (the
                   indexed history from the DEX's first market)
internal/openorders/
                   an account's open orders per denom and market, from the
                   orders table or the nodes, behind a 15 s cache
internal/labels/   the module and known account labels `migrate` seeds
internal/statesync/
                   the state sync: dirty sets, coalescing queue, workers,
                   safety-net timers, sync_jobs
internal/statesync/validators/
                   the validators set (explorer.validators and the
                   validator_owner labels)
internal/statesync/channels/
                   the IBC channels set (explorer.ibc_channels, ibc_escrow
                   labels, the counterparty of earlier transfers)
internal/statesync/denoms/
                   the denoms set (explorer.denoms, IBC denom resolution)
internal/statesync/registry/
                   the chain_registry set (explorer.chains, registry_assets)
internal/statesync/holders/
                   the holders set (token_holders, denoms.holders_count)
internal/statesync/orders/
                   the orders set (open orders closed when the node's
                   order book no longer lists them)
internal/statesync/prices/
                   the prices set (denoms.price_usd)
internal/statesync/dailystats/
                   the daily_stats set (the home charts' days, nightly)
                   and the aggregation the stats' live "today" shares
internal/chainregistry/
                   the Cosmos chain registry reader (GitHub contents API +
                   raw files)
internal/aggregator/
                   the BZE aggregator's /api/dex/tickers reader (BZE's 24-hour change)
internal/coingecko/
                   CoinGecko's USD prices and the cached feed behind /api/prices
internal/keybase/  the Keybase user lookup (validator logos by identity)
internal/rawcache/ in-memory LRU of raw node responses, archive fetch on a
                   miss
internal/status/   the status checker and its snapshot
internal/stakeflow/
                   the validators' stake movements of the last 7 days, in
                   memory, with their 24 h / 7 d running totals
internal/testutil/ acceptance-test helpers (database URL, Migrate)
internal/testutil/fakenode/
                   fake CometBFT RPC node and fake gRPC server for tests,
                   fixtures in testdata/ (gRPC ones in testdata/grpc/)
internal/testutil/fakeregistry/
                   fake chain registry (listings + recorded chain.json and
                   assetlist.json in testdata/)
e2e/               acceptance tests (build tag e2e)
scripts/           record-fixtures.sh, record-grpc-fixtures.sh (and its
                   bech32conv helper), record-registry-fixtures.sh
```

## Code and test rules

- **Dependencies are interfaces.** A component takes what it depends on (a
  database, a node, a listener, a transformer, an HTTP transport) as an
  interface declared next to it, so any of them can be swapped, mocked or
  replaced by a no-op. Concrete types are built only at the composition
  roots (`app/serve`, `app/cli`).
- **Unit tests and acceptance tests are both mandatory** wherever they are
  possible: unit tests with mocks of those interfaces (no network, no
  database), acceptance tests in `e2e/` against the compose PostgreSQL and the
  fake node.
- **Tests use the public API only.** Every test file is in the external
  `<package>_test` package (enforced by the `testpackage` linter, with no
  exceptions, `export_test.go` included). Nothing is exported just to be
  tested: unexported code is covered through the exported behaviour that
  uses it.

## HTTP API

Every route but `/health` lives under `/api/v1` and reads the explorer tables
only: never the CometBFT indexer's tables, never a node, except the raw-JSON
routes, which ask an archive node on a cache miss, and the live reads of the
account and parameters routes (the local node's gRPC). Conventions, which
every later route follows:

- **JSON** with snake_case names. Amounts and other big decimals are strings
  (coins are `[{"denom": "ubze", "amount": "2000"}]`), times RFC 3339 in UTC,
  and an absent value is `null`, never a missing field. Every block and
  transaction carries its height, index and hash.
- **Lists** answer `{"items": [...], "next_cursor": "<opaque>" | null}`.
  `limit` defaults to 25, maximum 100 (beyond is a 400). The cursor is the key
  of the last item (base64 of the key tuple); pass it back as `cursor` for the
  next page; `null` means the last page. Keyset queries carry a height
  predicate so PostgreSQL prunes the partitions above the cursor. The
  validator list, tens of rows, uses the row's position in its order as the
  key. Numbered pages exist in two places only: `/blocks?before=` (pages
  pinned to a top height) and an account's activity `?page=` (an offset
  bounded by that one address's rows); no global list takes an offset.
- **Errors** answer `{"error": {"code": "...", "message": "..."}}` with
  `bad_request` (400), `not_found` (404), `upstream_error` (502/504, for the
  routes that call a node) or `internal` (500, details only in the log).
- **Caching**: lists, search, validators, accounts and every error are
  `Cache-Control: no-store`; a block by height, its transfers and a found
  transaction are `public, max-age=60`: a committed height never changes, but
  the answer also joins what syncs later (denom symbols, labels, the
  counterparty chain, a block time), so it is never cached for good. A block
  whose proposer is not named yet (no synced validator has its consensus
  address) stays `no-store` until it is. A block's events and the raw node
  answers by height join nothing and are `public, max-age=31536000, immutable`,
  except a raw `/commit` that is not `"canonical": true` (a node's seen commit,
  asked at its tip), which is `public, max-age=60`.
- **CORS** only for the origins in `CORS_ALLOWED_ORIGINS` (the
  aggregator-compatible routes below answer any origin).
- **Request log**: one info line per request with method, path, status,
  duration and request id.

| Route | Answers |
| --- | --- |
| `GET /api/v1/blocks?cursor\|before&limit` | blocks, height descending (`before=<height>`: only those below it, exclusive — page p of a list pinned at top height H asks `before = H − (p−1)·limit + 1`; a `before` above the tip returns the newest; not a positive integer, or together with `cursor`, is a 400): height, time, hash, tx_count, tx_failed_count, proposer_cons_address, block_time_ms, size_bytes, `proposer` (`{operator_address, moniker}` of the validator with that consensus address, null while none is synced) |
| `GET /api/v1/blocks/{height}` | every column of the block plus `transactions` (height, tx_index, hash, success, msg_types, fee, first signer, gas_wanted, gas_used, `first_message` as on the transaction list), `transfers` (the block's own moves, as on the transaction page, `msg_index` null), `transfers_count` (every transfer at the height, the transactions' and the block's own: what `/blocks/{height}/transfers` lists) and `events` (its first 100 stored block events: seq, type, attrs) with `events_next_cursor` (null when they are all there); 400 unless a positive integer, 404 when not indexed |
| `GET /api/v1/blocks/{height}/events?cursor&limit` | the block's stored events in order (seq, type, attrs); immutable once the block is indexed; 404 when not indexed |
| `GET /api/v1/blocks/{height}/transfers?cursor&limit` | every transfer of the block, the block's own (`tx_index` -1) first, then the transactions' by tx_index and seq: `tx_index`, `tx_hash` (null for the block's own) and the transfer as on the transaction page; cached for a minute; 404 when not indexed |
| `GET /api/v1/block-events?type&cursor&limit` | the stored block events of every block, newest first (height, then seq, descending): `height`, `seq`, `time` (the block's), `type`, `kind`, `category`, `attrs`; `type` (a classified event type such as `bze.burner.CoinsBurnedEvent`, else 400) keeps one type and reads the `(type, height)` index; `no-store` |
| `GET /api/v1/txs?cursor&limit&status=success\|failed` | transactions, height and index descending: height, tx_index, hash, time, success, msg_count, msg_types, fee, first signer and `first_message` (`{type_url, module, body}` of the message at index 0, a lateral lookup bounded by the cursor's height; null for a transaction without messages, `body` null when it could not be decoded) |
| `GET /api/v1/txs/{hash}` | every column of the transaction (with `first_message` as on the list) plus `messages` (msg_index, type_url, sender, module, body, events) and `transfers` (seq, msg_index (null for the fee), kind `transfer`/`mint`/`burn`, sender, recipient, denom, amount in base units, `symbol`/`exponent` when the denom is known, `sender_label`/`recipient_label` `{name, kind}` when the address is labelled); the hash is 64 hex characters in any case (else 400); 404 when not indexed, which the UI shows as pending. `dex` is the order book outcome of its order messages, null when it has none: `placed` (the orders it rested on the book), `fills` (the fills it took, oldest first), `cancelled` (the orders it cancelled), since chain v8.3.0 `refunds` (`height`, `message_id`, `market_id`, `order_type`, `amount` = the base amount not executed, `refund` = the coin sent back `{denom, amount}` or null when it rounded to zero and only dust was kept, `reason` `no_match`/`unfilled_remainder`/`below_min_amount`), `skipped_cancels` (`height`, `message_id`, `market_id`, `order_type`, `order_id`, `reason` `order_not_found`) and `failed` (`message_id`, `message_type`, `height` of the latest block the engine failed to process it in; it stays queued), empty before that release, and `pending` (an order message it queued has no outcome indexed yet; the answer is then `no-store`, else cached for a minute). `ibc_transfers` are the cross-chain transfers it created (a `MsgTransfer`, a `MsgRecvPacket`) or settled (a relayer's acknowledgement or timeout, which may settle several), as on the account's cross-chain list, null when it has none; while one is `pending` the answer is `no-store` |
| `GET /api/v1/ibc/channels` | every IBC channel by number, one page: `channel_id`, `port_id`, `state` (`open`, `closed`, `init`, `tryopen`…), `client_status` (its light client: `active`, `expired`, `frozen`; null until asked — an expired or frozen client carries nothing until governance revives it), `client_id`, `connection_id`, `counterparty_channel_id`, `counterparty` (as on a transfer), `escrow_address` (the ICS-20 account holding what was sent over it), `updated_at`; `no-store` |
| `GET /api/v1/orders/{id}` | an order that rested on the book (`id` up to 24 digits, zero-filled): `order_id`, `market_id`, `owner`, `side`, `price`, `amount` (what rested, base units), `filled_amount`, `status` (`open`, `filled`, `cancelled`), `created_height`, `created_time`, `created_tx_hash` and `message_id` (null when unknown), `closed_height`, `closed_time`, plus `fill_count` and the newest 100 `fills`. 400 for anything but digits; 404 when not indexed; `no-store` |
| `GET /api/v1/validators?status=bonded\|unbonding\|unbonded\|all&cursor&limit` | validators (all statuses by default): the bonded ones by `rank`, then the others by tokens; rank, moniker, operator_address, tokens, voting_power_pct, commission_rate, uptime, jailed, status, `logo_url` (the Keybase picture of its identity, null without an identity or a picture), `missed_blocks` and `signed_blocks_window` (the slashing signing info, null until synced), `recent_votes` (`{voted, of}`: how many of the last ten proposals whose voting ended — fewer while the chain has fewer — its account voted on, whatever the option; also on `/validators/{op}`), `offline` / `missed_in_a_row` / `last_signed_height` (also on `/validators/{op}` and the Hall of Fame: an active — bonded, unjailed — validator's blocks not signed in a row up to the live cursor, counted from the latest of its last signed block, its last `bonded`/`unjailed` event and the start of the watch, `offline` from ten; all three null outside the active set, `last_signed_height` also null when it signed nothing since the watch began — the live writer records each block's signers in `validators.last_signed_height`, migration 000021, without history). `voting_power_pct` (share of the bonded tokens) and `uptime` (`1 − missed / window` of the slashing signing info) are percentages with five decimals; rank and voting power are null outside the active set; another status is a 400. The list also carries `summary`, the page's header from `chain_state`: `bonded_tokens`, `staked_share`, `reward_rate` (as in `/stats`), `unbonding_period` (seconds), `top5_share` (the five largest bonded validators' share of the bonded tokens, five decimals) and `max_validators` (the seats of the active set); each null until the chain state is synced |
| `GET /api/v1/validators/hall-of-fame` | the Hall of Fame, in one page (`next_cursor` null): the bonded, unjailed validators whose account voted, whatever the option, on every one of the last four proposals whose voting ended (all of them while the chain has fewer; nobody while none ended) that missed at most ten blocks of the slashing signing window and that came back at most ten minutes after the halt of the last decided upgrade (`upgrade_recovery.seconds_back`; an upgrade is decided once its recovery is computed or once the commits read reach a block ten minutes after its halt, since a later return is too late anyway; a validator outside the set at that upgrade does not qualify; no such condition while no upgrade is decided, and a newer upgrade not decided yet doesn't count), by voting power ascending; items as in `/validators`, except that `recent_votes` counts on those four proposals. `rules` states the rule for clients to word it: `{proposals: 4, max_missed_blocks: 10, signed_blocks_window, max_seconds_back: 600, upgrade: {height, name}}` (the window null until synced, `upgrade` null while no upgrade is decided) |
| `GET /api/v1/validators/{operator}` | every column of the validator (the list item plus account_address, consensus_address and key, description fields, tombstoned, jailed_until, commission limits and update time, min_self_delegation, self_delegation, delegator_count, missed_blocks, signed_blocks_window, first_seen_height/time, updated_at), `recent_blocks` (the last 10 it proposed), `events` (its last 20 `validator_events`, newest first, with `tx_hash` null for block-level and sync-found ones) and `votes` (the owner account's last 20 governance votes); 400 unless a `bzevaloper1…` address, 404 when not synced |
| `GET /api/v1/validators/{operator}/blocks?cursor&limit` | the blocks it proposed, height descending, as the block list; empty without a consensus address |
| `GET /api/v1/validators/stake-changes` | every validator's net stake change over the last 24 hours and the last 7 days: `{complete, as_of_height, items: {"<operator>": {"24h", "7d"}}}`, signed ubze strings (`"0"` for a window without net change); a validator without movements in the last 7 days is left out. Served from memory (see [Stake movements](#stake-movements)); `complete` is false while some height of the window is not indexed (the figures are then partial), and before the set is loaded (`as_of_height` 0, no items) |
| `GET /api/v1/validators/{operator}/movements?page&limit` | the validator's stake movements of the last 7 days, newest first, from memory: `{complete, window_from, page, total, items}`, page from 1, `limit` 10 by default, at most 50; each item has height, tx_hash, time, `kind` (`delegate`, `undelegate`, `redelegate_in`, `redelegate_out`, `cancel_unbonding`, `create` = the initial self-stake), delegator, amount (ubze, positive) and `counterparty_validator` (`{operator_address, moniker}`, moniker null when unknown; null except on redelegations). `window_from` is null before the set is loaded; 400 unless a `bzevaloper1…` address or for a bad page/limit, 404 when not synced |
| `GET /api/v1/modules` | `{"items": [{"address", "name", "module"}]}`: every module account the labels seed names (`labels` of kind `module`), by name; no pages. Cached `public, max-age=60`: only a migration's seed changes them |
| `GET /api/v1/accounts/{address}` | `address`, `label` (`{name, kind}` or null), `first_seen` (`{height, time}` of its first signed transaction, null when the explorer never saw it), `last_seen_height`, `tx_count`, `activity_count`, then the live state read from the node: `balances` (`{denom, amount, symbol, exponent}`, symbol and exponent from `denoms`, null when unknown), `delegations` and `unbonding` (`{validator, moniker, amount}`, unbonding with `completion_time`, one item per entry), `rewards` (`{validator, coins}`), `total_staked` (base units of the bond denom) and `total_rewards` (coins), rewards truncated to whole base units, and `live: {available}`. 400 unless a `bze1…` address; an address the explorer never saw is a 200 with null `first_seen`. When the node cannot be read the answer is still a 200 with the indexed part, empty live lists, null `total_staked` and `live.available` false |
| `GET /api/v1/accounts/{address}/activity?category=&cursor\|page&limit` | what each transaction or block did to the address, newest first (height, then tx_index, a block's own row after its transactions): `time`, `height`, `tx_hash` (null for a block-level row), `kind`, `category`, `is_signer`, `success`, `counterparty` (`{address, label, moniker}` when there is exactly one, else null), `deltas` (the net balance change per denom, fee included, as balances with symbol and exponent), `msg_types`, `details` (the typed events naming the address on block-level rows, else null) and `fee` (when the address signed or paid the transaction, else null). `category` filters on `sent`, `received`, `staking`, `dex`, `governance`, `tokens`, `rewards`, `burner`, `cross_chain` or `other`; anything else, or an address that is not `bze1…`, is a 400; no activity is an empty list. `page=<n>` (n ≥ 1, else 400; not with `cursor`) skips the newest `(n−1)·limit` rows and adds `total`: the address's row count (`activity_count`, 0 when the explorer never saw it) or, with `category`, that category's count; a page past the end is an empty list with the total; `next_cursor` still leads to the older rows. Without `page` there is no `total` |
| `GET /api/v1/accounts/{address}/orders?status=&cursor&limit` | the orders the address rested on the book, newest first (created height, then id), as the order route without fills; `status` filters on `open`, `filled` or `cancelled`. A taker order that filled completely never rested, so it is only in the fills. 400 unless a `bze1…` address or a known status |
| `GET /api/v1/accounts/{address}/ibc?direction=&cursor&limit` | the cross-chain transfers the address sent (`direction=out`), received (`in`) or both (no direction), newest first (creating transaction, then channel number and sequence): `channel_id` (BZE's side), `sequence`, `direction`, `status` (out: `pending`, `delivered`, `refunded_rejected`, `refunded_timeout`; in: `received`, `rejected`), `expired` (pending past `timeout_time`, waiting for the relayer to bring the timeout back), `sender`, `receiver`, `denom` (the BZE denom sent; for an inbound transfer the packet denom before BZE's `ibc/` hash), `amount`, `memo`, `counterparty` (`{chain_id, pretty_name, logo_url, explorer_account_url, explorer_tx_url}` from the chain registry, the templates with `${accountAddress}` / `${txHash}`; null fields when unknown), `created_height`/`created_tx_hash`/`created_time`, `timeout_height` (`revision-height`, null for none), `timeout_time`, `result_height`/`result_tx_hash`/`result_time` (null while pending) and `ack_error` (the other chain's error, or `blocked inbound denom`). 400 unless a `bze1…` address or a known direction |
| `GET /api/v1/accounts/{address}/fills?cursor&limit` | the fills where the address is the maker or the taker, newest first (height, then seq): `height`, `time`, `order_id` (the resting order), `market_id`, `side` (of the resting order), `price`, `amount`, `maker`, `taker`, `taker_tx_hash` and `taker_message_id` (null when unknown). 400 unless a `bze1…` address |
| `GET /api/v1/accounts/{address}/open-orders` | what the address has locked in open DEX orders: `source` (`index` or `node`, for debugging), `height` (the height the answer describes), `orders_count`, `markets_count` and `assets`, one per locked denom sorted by denom: `denom`, `amount` (base units, a string), `orders_count` and `markets` (largest amount first: `market_id`, `base`, `quote`, `amount`, `orders_count`). A sell locks its remaining amount of the base denom, a buy trunc(remaining × price) of the quote denom, truncated per order as the chain does, so a market's `orders_count` under an asset counts only the orders locking that asset. Read from the orders table when its history is complete, from the node otherwise (see Open orders below); cached 15 s per address; no-store. An account without open orders has empty `assets` and zero counts. 400 unless a `bze1…` address; 502 when neither node answers |
| `GET /api/v1/tokens?kind=native\|factory\|ibc\|lp\|unknown&cursor&limit` | every denom by kind (native, factory, ibc, lp, unknown) then symbol: denom, symbol, name, kind, exponent, supply (base units), holders_count (owners of at least one display unit, null until the holders job), price_usd (the aggregator's USD price, null until the prices job and for a denom it does not price), price_change_24h_pct (`ubze` only: BZE's 24-hour change in percent, four decimals, from the BZE/USDC pool's ticker; null for every other denom), halted, logo_url, `origin_chain` (an IBC denom's `{chain_id, name, logo_url, status}`, name, logo and status (`live`, `upcoming`, `killed` = shut down) from the chain registry and null for a chain it does not know; null for other denoms and until the denom's channel is known), `ibc_hops` (an IBC denom's number of hops, the port/channel pairs of its path; null for other denoms and until traced), `via_chain` (a multi-hop IBC denom's first hop chain, the counterparty of the BZE channel, shaped like `origin_chain`; null for one hop and other denoms), `lp_pair` (an LP denom's pool assets `{base, quote}`, both denoms; null for other denoms and when unknown), `created_time` (the block time of its `created` token event, null without one), `ibc_channel` (BZE's channel of an IBC denom's first hop: `{channel_id, state, client_status}`; null for other denoms and until known), `stranded` (an IBC denom that can no longer travel back: its channel closed, its client expired or frozen, or its origin or via chain `killed`); another kind is a 400 |
| `GET /api/v1/tokens/{denom}` | every column of the denom (the list item plus description, the IBC origin fields, creator and admin with their labels (admin null once renounced), created_height/tx_hash/time from its `created` event, website, markets, raw bank `metadata`, updated_at), `events` (its last 20 token events, newest first), `events_next_cursor` and `transfers_24h` (`{count, amount}`: the transfers rows of the denom in the 24 hours before the latest indexed block, like `txs_24h` on `/stats`, the amount summed in base units as a string; read through the `(denom, height)` index); the denom is URL-encoded (`factory%2Fbze1…%2Fuhoney`) since it contains `/`; 404 when unknown |
| `GET /api/v1/tokens/{denom}/events?cursor&limit` | the denom's token events, newest first: height, tx_index, seq, tx_hash (null for a block-level halt), kind, actor (and `actor_label`), amount, details, time |
| `GET /api/v1/tokens/{denom}/transfers?cursor&limit` | every indexed move of the denom, newest first: height, tx_index, tx_hash, time and the transfer as on the transaction page |
| `GET /api/v1/tokens/{denom}/holders?cursor&limit` | the last holders snapshot, largest balance first: rank, address, `label`, balance (base units) and `share_pct` (of the supply, five decimals, null without a supply); every non-zero balance, refreshed hourly |
| `GET /api/v1/token?denom=`, `/api/v1/token/events?denom=`, `/api/v1/token/transfers?denom=`, `/api/v1/token/holders?denom=` | the same four with the denom as a query value, the chain's own convention for denoms with `/` |
| `GET /api/v1/proposals?status=deposit_period\|voting_period\|passed\|rejected\|failed\|canceled\|dropped&cursor&limit` | every proposal, id descending: id, title, kind (`software_upgrade`, `community_pool_spend`, `parameter_change`, `cointrunk_publisher`, `ibc_client_update`, `text`, `other`), status, expedited, submit/deposit-end/voting-start/voting-end times, `tally` (`{yes, no, abstain, no_with_veto}` in base units, null until synced), `turnout_pct` (the tally's sum over the bonded tokens it was measured against, five decimals, null without them) and `tally_source` (where those bonded tokens come from: `sync` while the proposal voted, `archive` from the snapshots command, historical; null without them); another status is a 400 |
| `GET /api/v1/proposals/{id}` | the list item plus summary, metadata, proposer (and `proposer_label`), message_types, `messages` (proto JSON; a legacy content inside its `MsgExecLegacyContent`), total_deposit, tally_bonded_tokens, tally_updated_at, submit_height and submit_tx_hash (null for a proposal submitted below the indexed range), resolved_height, `validators_voted` (`{voted, total}`: bonded validators whose owner voted, of all bonded validators), `votes_count` (the accounts that voted), `active_set` (`{height, size, non_voters_count}`: the CometBFT set the tally counted, its size and how many of it did not vote; null until recorded, always null for a proposal that never had a tally), `upgrade` (a passed software upgrade: `{height, halt_time, resume_time, watched, downtime_seconds}`, the plan height, when the chain halted and resumed, whether the halt watch saw it and the halt's length; the times null until read; null for any other proposal) and updated_at; 404 when unknown |
| `GET /api/v1/proposals/{id}/votes?option=yes\|no\|abstain\|no_with_veto\|weighted&voter=validators&cursor&limit` | every vote the explorer indexed (the chain deletes them once tallied), newest first (`voter=validators` keeps the votes of accounts owning a validator), one per voter (the latest): voter (and `voter_label`), option (null for a split vote; `weighted` lists those), options (the weights as SDK v0.50 encodes them), height, tx_index, tx_hash, time, and for a validator owner `validator` (moniker), `validator_operator` and `voting_power_pct` (its current share), null for other voters; `source` is `event`, or `archive` for a vote the snapshots command read from the state before the tally (historical: tx_index -1, tx_hash null) |
| `GET /api/v1/proposals/{id}/non-voters?cursor&limit` | the validators of the set the proposal's tally counted that did not vote (see State sync, Proposals' non-voters), by voting power descending then consensus address: `operator_address` and `moniker` (null when no validator is known with the address), `consensus_address` (upper-case hex), `voting_power` (consensus power at the tally) and `power_pct` (share of the set's power, five decimals); `no-store`; an empty list until the set is recorded; 404 when the proposal is unknown |
| `GET /api/v1/upgrades/live` | while the chain is halted for a passed software upgrade (the halt watch is armed: the node sits at U−1 or U with a block older than 30 s), what its latest poll of `/consensus_state` saw: `halted: true`, `height` (U), `halt_height` (U+1), `proposal_id`, `name`, `round`, `halted_since` (the node's latest block time), `checked_at`, consensus powers as strings `total_power`, `back_power`, `prevote_power`, `precommit_power` (the chain resumes once precommits pass two thirds), and `members`, the set at U+1 by power: `consensus_address`, `operator_address` and `moniker` (null when unknown), `voting_power`, `back` (voted at U+1, any round), `prevote`/`precommit` (in the current round); `{"halted": false}` otherwise, including when the watch is off or the explorer is merely behind; served from memory, `no-store` |
| `GET /api/v1/upgrades/{height}` | the software upgrade planned at that height (see State sync, Upgrade recovery): height, proposal_id, name, `halt_time`, `resume_time`, `downtime_seconds`, `watched`, `active_set_size`, `window_end_height` (the last height read), `computed_at` (null while still reading), and `members`, every validator of the set at height+1 sorted by `back_time` (null last), then power: `operator_address` and `moniker` (null when unknown), `consensus_address`, `voting_power`, `power_pct`, `back_time`, `back_source` (`halt_vote` or `commit`), `vote_time` (its own time on that vote or signature), `seconds_back` (after the halt), `first_signed_height` and `blocks_needed` (commit only); `no-store`; 404 when no passed upgrade is planned there |
| `GET /api/v1/proposals/{id}/deposits?cursor&limit` | every deposit, the initial one included, newest first: depositor (and `depositor_label`), amount (coins), height, tx_index, tx_hash, time and `source`: `event`, or `archive` for a depositor's total the snapshots command read from the state before the tally (historical: tx_index -1, tx_hash null; left out once that depositor's transactions are indexed) |
| `GET /api/v1/search?q=` | `{"results": [{"type", "id", "label"}]}`: digits find an indexed block, 64 hex characters an indexed transaction, a `bze1…` address an account (always returned, with `indexed` true or false), a `bzevaloper1…` address a known validator; any other text of two characters or more the validators whose moniker contains it and the labelled accounts whose name contains it (any case, five of each, validators first, accounts labelled by name); no match is an empty list; an empty `q` is a 400 |
| `GET /api/v1/stats` | the home tiles, from the indexed blocks and transactions and the `chain_state` rows (no node call): `latest_height`, `latest_time`, `avg_block_time_ms` (the average `block_time_ms` of the last hour) and `txs_24h` (transactions of the last 24 hours), both windows measured back from the latest indexed block; `block_time_windows`, the average block time over the last 10,000, 30,000, 50,000 and 100,000 blocks in that order (`[{blocks, from_height, avg_block_time_ms}]`: (time of the latest indexed block − time of block `from_height`) ÷ `blocks`, whole milliseconds, read from the two blocks by height; `avg_block_time_ms` null when block `from_height` is not indexed or below 1; `[]` before the first block), for estimates further ahead than the one-hour `avg_block_time_ms`; `bonded_tokens`, `supply` and `community_pool` (whole base units of `ubze`), `staked_share` (bonded over supply), `inflation`, `reward_rate` (inflation × (1 − community tax) ÷ staked share: the yearly staking reward before commission), all three percentages with five decimals; `validators` (`{active, total}`), `max_validators`, `unbonding_period` (seconds), `price_usd` and `price_change_24h_pct` of `ubze` (null until the prices job); `daily`, the stored days of the last 30 closed UTC days (`?days=` from 1 to 365, otherwise a 400), oldest first, a day without indexed blocks absent; and `today`, the current UTC day so far computed live with the same query (null while it has no indexed block). Each day is `{day, blocks, txs, txs_failed, active_addresses, new_addresses, transfers, fees, avg_block_time_ms}` (see Daily statistics under State sync). Every chain-state field is null until its row exists |
| `GET /api/v1/params` | the live parameters of every module the parameters page shows (txfeecollector, staking, mint, distribution, slashing, gov, tradebin, tokenfactory, rewards, burner, cointrunk), keyed by module: `{"<module>": {"params": <proto JSON as the node answers>, "last_change": {"height", "time", "proposal_id"} \| null}}`. Values are read from the local node's gRPC through a 30 s in-process cache, so the page never disagrees with the chain; `last_change` is the module's latest recorded change (null before one), its `proposal_id` null for a change no proposal explains. A module the node fails to answer is left out; none answering is a 502 |
| `GET /api/v1/params/halted-denoms` | the denoms governance halted on the DEX (tradebin `MsgHaltDenoms`; no trading, swaps, new pools or fee payment in them), as the node lists them: `{"items": ["<denom>", …]}`, empty when none. Not a module parameter (tradebin keeps them in a store of their own) but the parameters page lists them; read in one page from the local node's gRPC through the same 30 s cache. A node from before the halt answers an empty list; a node that fails is a 502 |
| `GET /api/v1/status` | `{"live_fill": {"healthy", "checked_at", "db_height", "node_height", "archive_height"}, "back_fill": {"status", "oldest_height"}}`, always 200 and `no-store`; before the first check `healthy` is false and `checked_at` null; a height that could not be read is null |

The status checker runs inside `serve` (with or without the indexer): it
ticks at start and then every `STATUS_INTERVAL`, reading
`indexer_state.last_indexed_height`, `min(height)` of `explorer.blocks`, and
`/status` of the local node and of the archive (the retry URL when the
primary fails; 5 s per call). `healthy` needs all three heights, a spread of
at most `STATUS_HEIGHT_TOLERANCE`, and the explorer's height above the
previous tick's; the first tick after a start judges the spread only.
`back_fill.status` is `finished` when the main backfill job's checkpoint is
`done`, or when the backfill is disabled and never ran (no checkpoint);
otherwise (`running`, `paused`, `error`, or enabled and waiting) it is
`in_progress`: older history is still owed.

| Raw route | Answers |
| --- | --- |
| `GET /api/v1/raw/block/{height}` | the node's `/block?height=` response body, verbatim |
| `GET /api/v1/raw/block_results/{height}` | the node's `/block_results?height=` response body, verbatim |
| `GET /api/v1/raw/commit/{height}` | the node's `/commit?height=` response body, verbatim |
| `GET /api/v1/raw/tx/{hash}` | `{"height", "index", "tx", "tx_result"}`: the base64 transaction from `/block` `data.txs[index]` and `/block_results` `txs_results[index]`, at the height and index `explorer.transactions` holds; 404 when the hash is not indexed |

A height is a positive integer (else 400) and is proxied whether the explorer
indexed it or not. Successes are `immutable`, but a commit body without
`"canonical": true` (the seen commit of a node's tip, see "Live indexer") is
`public, max-age=60`, so a browser picks up the canonical one; when no archive node serves the
height (unreachable, an error, above its tip) the answer is 502
`upstream_error` and nothing is cached. The cache keeps one LRU per route of
at most `RAW_CACHE_MAX_ENTRIES` entries, each living `RAW_CACHE_TTL` from the
moment it was stored; an entry is never refreshed (a height never changes),
except a seen commit, which the live indexer replaces with the canonical one.
The live indexer puts the three bodies of every height it writes, so recent
heights never reach the archive. A miss fetches from `ARCHIVE_RPC_URL`, then
once from `ARCHIVE_RPC_RETRY_URL` (or the same URL again); concurrent misses
for one height share one fetch, which outlives a request that gives up.

## Aggregator-compatible endpoints

The explorer also serves the BZE aggregator API's public endpoints, at the
aggregator's exact paths and with its exact answers (fields, JSON types,
order, status codes), so the dapps, getbze.com and CoinGecko can switch to
it unchanged. They live in their own route group, `server.CompatPrefix`
(`/api`), mounted next to `/api/v1`, not inside it: every endpoint ported
from the aggregator is added to that group, with controllers of its own
(`app/controller/compat_*.go`, shapes in `app/dto/compat.go`). None of the
`/api/v1` conventions apply: no `{items,next_cursor}` envelope, no
pagination, no cache headers the aggregator does not send. They always
answer CORS with `Access-Control-Allow-Origin: *`, whatever
`CORS_ALLOWED_ORIGINS` says, because browsers call them directly the way
they call the aggregator; `/api/v1` keeps its configured origins. An empty
result is `[]`, never `null`.

| Route | Answers |
| --- | --- |
| `GET /api/prices` | `[{"denom":"<coingecko id>","price":<number>,"price_denom":"usd"}]`, one entry per id of `COINGECKO_PRICE_IDS` CoinGecko prices, in that order |
| `GET /api/articles/medium` | `[{"title","url","picture_url","description","publish_date","author_name"}]`, the first six articles of `MEDIUM_FEED_URL`, in the feed's order |
| `GET /api/supply/total?denom=` | plain text: the denom's bank supply in display units, 2 decimals (`denom` defaults to `ubze`) |
| `GET /api/supply/circulating?denom=` | plain text: for `ubze` the total minus the community pool, for any other denom its total |

**Prices.** `internal/coingecko`: the client calls
`{COINGECKO_HOST}/api/v3/simple/price?ids=<ids>&vs_currencies=usd`, and the
feed keeps the answer for 180 s; when CoinGecko fails (or prices none of the
ids) the last read is served for up to 24 hours after it, then `[]` (the
aggregator answers `null`). The route is always 200. The prices job of the
state sync reads the same feed. `serve` and `sync-state` each hold one.

**Medium articles.** `internal/medium`: the client reads the RSS feed at
`MEDIUM_FEED_URL` (default `https://medium.com/feed/bzedge-community`) with
gofeed, as the aggregator does, and keeps its first six items: `picture_url`
is the first `<img>` src of the content (the item description when there is
no content) when it holds `https://cdn`, else `""`; `description` is the
content without HTML (bluemonday's strict policy, which escapes entities
again) cut at 150 **bytes**, as the aggregator cuts, then `...`, so a
multi-byte character across the cut becomes U+FFFD; `publish_date` is the
item's date in RFC 3339, UTC. The feed keeps the answer for 600 s. When the
feed fails (asked again at most every minute) the last read is served for up
to 24 hours after it, then `[]`; the aggregator answers `[]` for 600 s. An
empty feed is `[]` (the aggregator answers `null`). The route is always 200.

**Supply.** `internal/supply`, served only with `NODE_GRPC_ADDR`: bank
`SupplyOf` for the one denom (the aggregator lists one page of 5000 supplies
and misses the denoms beyond it), distribution `CommunityPool` for `ubze`'s
circulating supply, the display exponent from `explorer.denoms` (`ubze` is 6
even before the first sync; a denom the chain minted but the sync has not
written yet is 0). The amount is formatted with exactly 2 decimals rounded
half up with integer arithmetic, and the circulating supply is the exact
difference rounded once (the aggregator subtracts a float from the rounded
total, so the two can differ by 0.01). Each answer is kept 600 s per denom
and route; when the node or the database fails the last read is served for up
to 24 hours after it, then a 503 `supply is unavailable, try again later`
(the aggregator answers `0`). A 400 carries the aggregator's message: `denom
<denom> not found in registry` for a denom the explorer does not know and the
chain has no supply of (the aggregator answers a 500), `<denom> has an
unsupported display exponent` outside 0–18. Answers are `text/plain;
charset=UTF-8`, like the aggregator's.

## Migrations

`bze-scan migrate` manages the schema. The database must already hold the
CometBFT `psql` sink schema in `public` (a node with the psql indexer enabled,
or `docker/compose.yml` for development); without `public.blocks` it stops
before creating anything.

```
bze-scan migrate            # same as up
bze-scan migrate up         # apply pending migrations, then the post-migration steps
bze-scan migrate down 2     # revert the last two migrations
bze-scan migrate version    # print the current version (0 when none)
```

- The migrations are `migrations/NNNNNN_<name>.up.sql` and `.down.sql`,
  embedded in the binary and applied with golang-migrate: the schema, the
  notification trigger, the history tables, the state tables, the
  operational tables, the functions (`ensure_partitions`,
  `reclassify_unknown`), the DEX lookups (fills by maker, the outcome of
  a transaction, an order's cancel), the IBC lookups (the transfers a
  transaction settled), the order attribution lookups (unattributed
  outcomes, open orders per owner) and the daily statistics lookups
  (addresses active at a height range, accounts first seen in a window).
- Everything the explorer owns lives in the `explorer` schema. The version
  table is `explorer_migrations.schema_migrations`, in a schema of its own so
  that `down` to version 0 can drop `explorer` entirely.
- The only object attached to the sink is the trigger `trg_notify_block` on
  `public.blocks`: `pg_notify('explorer_block', height)` after each insert,
  which wakes the live indexer. No `down` touches the sink's tables or rows.
- After `up`, the post-migration Go steps run in order (`postMigrate` in
  `migrations/steps.go`; each must be idempotent). The first creates the
  height partitions of the seven history tables (`blocks`, `transactions`,
  `messages`, `transfers`, `account_activity`, `block_events`,
  `order_fills`): one per 1,000,000 heights, `_p000000` to `_p000049`
  (`blocks_p000024` holds [24,000,000, 25,000,000)). The second mirrors the
  classification of `internal/classify` into `explorer.message_kinds` and
  `explorer.block_event_kinds` (upsert every entry, delete the rows without
  one), then runs `explorer.reclassify_unknown()` so activity stored as
  `other` picks up the entries added since. The third seeds
  `explorer.labels` from `internal/labels` (`source = seed`): one `module`
  row per module account the chain's app declares (the address derived as
  `authtypes.NewModuleAddress` does) and the `known` accounts (none yet);
  seed rows no longer listed are deleted. The state sync adds the
  `validator_owner` rows. The fourth, `message_events`, runs while
  migration 000018's `explorer.pending_rewrites` row exists: it filters the
  stored `explorer.messages.events` through `explorer.kept_events` (see
  "Transactions and messages"), one UPDATE per 1,000,000 heights, each its
  own transaction (a migration file runs in one), then deletes the row; an
  interrupted run resumes at the next `up`, and once the row is gone the
  step reads nothing.
- Running `up` again changes nothing, so it can run on every deploy.

Acceptance tests call `testutil.Migrate(t)` to bring the compose database up
to date; tests that need an untouched database create their own.

## Live indexer

Runs inside `serve` unless `INDEXER_ENABLED=false`. One per database.

- A dedicated connection runs `LISTEN explorer_block`. Each notification (the
  height the sink just committed) and each (re)connect starts a pass: the
  target is the greater of the notified height and the node's `/status`
  height, and every height above `indexer_state.last_indexed_height` up to
  the target is indexed in order. A notification lost while disconnected is
  recovered by the pass that follows the reconnect. Reconnects back off from
  1 s to 30 s.
- The very first pass, with no cursor yet, indexes only the target: the live
  side starts at the head of the chain and the backfill owns what lies below.
  That first height is stored once as `indexer_state.live_floor`; the live
  side never indexes below it.
- Per height: `/block`, `/block_results` and `/commit` from the local node,
  the transformer, then one transaction that bulk-inserts the
  `explorer.transactions` and `explorer.messages` rows, inserts the
  `explorer.blocks` row last (every insert `ON CONFLICT DO NOTHING`;
  `block_time_ms` from the previous row when it is indexed), sets the live
  floor at the first write, and moves the cursor with `GREATEST`. A blocks
  row therefore proves its height is complete. After the commit the three
  response bodies go to the raw-JSON cache. When a height enters the last existing partition, the same
  transaction calls `explorer.ensure_partitions(height, height + 20000000)`.
- Heights below the node's `earliest_block_height` (an outage longer than
  its retained window) go to the catch-up job before the pass reads the
  rest from the node: the backfill pipeline walking upward through the
  archive (same writer pool, rate limiter and retries, failures recorded
  with source `live`), skipping heights already present, which then moves
  the cursor past them. A failed catch-up leaves the cursor and reconnects,
  so the next pass starts over.
- A height is tried three times (waits of 0.5 s and 2 s). Then, or at once
  when the node reports the height pruned, it is recorded in
  `explorer.index_failures` with source `live`, logged, and the cursor moves
  past it. A pass stops (and reconnects) when the database or the node
  cannot be reached, without recording anything or moving the cursor: a
  fetch the node did not answer (connection refused or reset, a timeout,
  HTTP 429/502/503/504, or Cloudflare's 520–524, from a proxy) is checked
  with a `/status` call, and when that fails too the node is down and the
  next pass resumes at the same height. So a node restart, even in the
  middle of a catch-up, never turns into recorded failures. A node that
  answers `/status` but not a height is a failed attempt at that height.
- Seen vs canonical commits. A height is usually indexed while it is the
  node's tip, and then `/commit` answers the node's *seen* commit
  (`"canonical": false`): only the precommits that arrived before the node
  moved on, often just the +2/3 it needed. The *canonical* commit, block
  H+1's `last_commit`, has every signature that made it in time, and the node
  answers it (`"canonical": true`) once H+1 exists. So the block is written
  at once with the seen figures, and when the next height of the same
  session is written the indexer re-reads the seen height's commit, updates
  its `signatures_count` and `signatures_power_pct` (the same expression as
  the insert) and replaces its commit in the raw-JSON cache. A failed or
  still-seen re-read only logs a warning: nothing is recorded and the live
  path goes on. Each session (start, reconnect) begins by treating the cursor
  as seen, since the height written just before it may be, and re-reads it
  once too. Backfill, catch-up and reindex read old heights, whose
  commits are canonical.
- On shutdown the height in flight gets up to 10 s to finish.
- Nothing is read from the sink's tables: the notification carries the
  height, the node supplies the data.

## Backfill

History from the archive node, from the live floor down to `BACKFILL_FLOOR`.
`bze-scan backfill` runs it standalone; `serve` runs the same job as one of
its components when `BACKFILL_ENABLED=true` (a job that fails or is locked
elsewhere ends without stopping the process, and the next start resumes it).

- **Lock.** One backfill per database: the job first takes a session-level
  `pg_try_advisory_lock` keyed on its name on a connection of its own. A
  process that cannot take it logs and skips (the command exits 1).
- **Ceiling and floor.** The ceiling is `indexer_state.live_floor - 1`; on a
  fresh install the job polls every 10 s until the live indexer has recorded
  the floor. The floor is a height, `genesis` (1) or a date, resolved by a
  binary search over `/block` header times on the archive (about 25 probes).
- **Pipeline.** The dispatcher walks the heights down and hands each to a
  worker the moment one of X semaphore slots frees. A worker fetches
  `/block`, `/block_results` and `/commit` under one rate limiter shared by
  every worker, retries four times with a backoff (5 s, 25 s, 125 s, 10 min:
  about 12.5 minutes in all, so an archive that struggles for a while leaves
  no failed heights) against `ARCHIVE_RPC_RETRY_URL` (or the primary again),
  runs the archive adapter (see "Old event formats") and the transformer,
  and sends one result into the writer's channel (capacity X). Its slot
  frees once the result is queued, so memory holds at most X in flight, X
  queued and M held.
- **Writer.** A batch writer of its own (own pool, never the live writer's)
  holds results in arrival order and flushes when it holds M heights, after
  `BACKFILL_QUIET` without a result, and when the channel closes after the
  last worker, even on shutdown: nothing fetched is left unwritten. A flush
  is one transaction sent as one pgx batch: multi-row `INSERT … ON CONFLICT
  DO NOTHING` per table in chunks of 1,000 rows, `block_time_ms` filled for
  the flushed heights and the one above them wherever the previous block is
  now present, then the `PostFlush` hooks. It never writes the live floor,
  the cursor, the sink or the state tables. A failed flush is fatal (the
  database is gone): the run stops with checkpoint status `error`.
- **Failures.** After the retries a height is upserted into
  `explorer.index_failures` with source `backfill`, logged, and skipped.
- **Outages.** An unreachable archive is not a failure of its heights. When
  a fetch gets no answer (connection refused or reset, a timeout, HTTP
  429/502/503/504, or Cloudflare's 520–524, from a proxy) and the node's
  `/status` does not answer either, the worker waits (1 s, doubling up to
  1 min) and fetches the same height again without spending an attempt, for
  as long as the outage lasts. Every worker waits, so the run pauses and
  resumes on its own; the catch-up and the reindex behave the same (Ctrl-C
  stops a waiting reindex). A node that answers `/status` but not a height
  (a slow block timing out, say) is an ordinary failed attempt.
- **Checkpoint.** `explorer.backfill_checkpoints` row `main` (ceiling, floor,
  `lowest_dispatched`, `resume_height`, `blocks_done`, status, last error),
  saved every 5 s and after every flush. `resume_height` is the highest
  height still pending (in flight, queued or held unflushed), so every height
  above it is written or recorded: a height held up by retries can lie far
  above `lowest_dispatched`, and a stop drops the heights in flight. On
  restart the dispatcher starts at `resume_height` and skips the heights
  already in `explorer.blocks` (one query per 1,000 heights). Status:
  `running`, `done` at the floor, `error` after a fatal error (resumed at the
  next start), `paused` while `BACKFILL_ENABLED=false`. A done job is not
  rerun unless the floor moves lower.

### Old event formats

The release history behind it is per chain (`archive.ForChain(CHAIN_ID)`): the
same height is a different release on mainnet `beezee-1` and on testnet
`bzetestnet-3`, which started on v8.0.2, so every testnet height has the
current format. Backfill, reindex and the state sync refuse to start for a
chain id without a table.

Mainnet heights before v8.0.0 (height 20,237,800) were produced by Cosmos SDK
0.44/0.45 on Tendermint/CometBFT 0.34. On the archive path only (backfill,
catch-up, reindex; never the live path), `internal/archive` normalises them
to the current format before the transformer sees them: `msg_index` rebuilt
from message order, the leading `message` event rebuilt as SDK 0.50 builds it
(legacy action names such as `create_order` mapped to type URLs), typed
events renamed from the old proto packages (`bze.tradebin.v1.*` →
`bze.tradebin.*`), `voter`/`depositor`/`proposer` and the vote option array
added to the governance events, `amount`/`denom`/`memo` to `ibc_transfer`.
The codec decodes the pre-v8 BZE type URLs (`/bze.tradebin.v1.MsgCreateOrder`)
into today's messages, so the rows carry the current type URL; the scavenge
module, removed in v8, keeps its old URL with a NULL body. A height whose shape
does not match its generation fails into `index_failures` with
`unknown event generation`. The generation table, how its heights were
verified and every difference are in the package documentation
(`internal/archive/generation.go`). The archive node must run CometBFT 0.38:
it converts the legacy ABCI responses itself, and an older node's answer is
rejected rather than guessed at.

v8.0.x (heights 20,237,800 to 22,551,809) has the current format but reports
part of its finalize-block events twice: the chain ran its EndBlock and
epoch-hook work (raffle draws, burns, reward distributions, the DEX queue)
through a helper that re-emitted the events SDK 0.50 already re-emits, until
v8.1.0 dropped it. The coins moved once. On the same path the adapter drops
the second copy of every run of finalize-block events immediately repeated,
so those heights store each transfer, fill and event once; at each position
the longest repeated run wins. Transactions' own events are not affected.

### Transactions and messages

Transactions are decoded with the chain's own Go types: the backend imports
`github.com/bze-alphateam/bze` (pinned to the v8.2.0 release line, a superset
of the messages mainnet accepts today) and builds its codec from the chain's
app configuration without starting the app, plus the IBC modules the chain
registers by hand. `go.mod` repeats the chain's `replace` directives.

- `hash` is the SHA-256 of the raw bytes, upper-case hex, as the node and the
  sink compute it.
- `fee`, `fee_payer` and `signers` come from the ante handler's `tx` events
  (`fee`, `fee_payer`, `acc_seq`), which a failed transaction emits too; the
  decoded transaction fills in what the events lack. `memo`, `msg_count` and
  `msg_types` come from the decoded bytes, so failed transactions have them.
- One `messages` row per message: the type URL, `sender` and `module` from
  the message's first `message` event (else the first signer), `events` = the
  transaction's events whose `msg_index` is the message's, in emission order,
  and `body` = the message as proto JSON. A failed transaction's messages
  have a body and no events. An authz `MsgExec` is one row whose body carries
  the nested messages.
- `events` leaves out what the explorer keeps elsewhere
  (`transform.UnstoredEventTypes` / `UnstoredEventAttributes`, mirrored by
  the SQL function `explorer.kept_events`): the `message` event (the
  `type_url`, `module` and `sender` columns), `transfer`, `coin_spent` and
  `coin_received` (the `explorer.transfers` rows built from them) and the
  `header` attribute of `update_client` (a hex copy of the header the body
  carries). They are dropped only from what is stored: transfers and account
  activity read the full events first. Every other event is kept, so a
  future feature can read it without a reindex. Should one of the dropped
  ones be needed, find the heights of the messages concerned
  (`SELECT DISTINCT height FROM explorer.messages WHERE type_url = …`), stop
  dropping it and run `bze-scan reindex --heights …` over them.
- Attribute values of typed events (`bze.*`) are JSON-decoded once, here;
  SDK attribute values stay strings. Coin strings such as
  `70255ubze,12ibc/ED07…` are split by `chain.ParseCoins` (amount = leading
  digits, the denom may contain `/`).
- A message type the chain's registry does not know never fails a height: the
  row keeps the type URL with a NULL body, and the indexer logs a warning.
  Bytes that are not a transaction at all still get their `transactions` row
  from the block results.

### Transfers and block events

`explorer.transfers` is where the funds went, one row per coin:

- In a transaction, its `transfer` (`kind = transfer`), `coinbase` (`mint`,
  recipient only) and `burn` (`burn`, sender only) events in emission order,
  `seq` counting from 0 and `msg_index` from the event (NULL for the ante
  handler's fee transfer). A failed transaction keeps its fee row only. A
  `transfer` without a sender (a `MsgMultiSend` output) takes the sender of
  the message event before it.
- In the block's finalize events, the same with `tx_index = -1`, minus the
  routine moves the `blocks` row already summarises: the mint module's
  `coinbase`, its transfer to `fee_collector` and `fee_collector`'s transfer
  to `distribution`, recognised by the module addresses. Order settlements,
  unbonding completions, payouts, burns and prizes are rows.

`explorer.block_events` keeps the finalize events whose type the block-event
classification (`internal/classify`) lists, `seq` being the event's position
in the list and `attrs` its attributes (typed values decoded). Routine events
(`mint`, `commission`, `rewards`, `proposer_reward`, `coin_spent`,
`coin_received`, `coinbase`, `transfer`, `message`, `liveness`) are never
stored. The classification lists every typed event the BZE modules emit
outside a transaction (checked against the chain's keepers; events of
message handlers only, such as `DenomRewardDistributionEvent`, never reach
the finalize-block events), and the transformer logs a warning for a
`bze.*` block event it does not list, which is then neither stored nor
turned into activity. A type added to the classification later needs a
reindex of the range. The wording a page shows ("transaction fee", "paid to the seller") is
derived at read time; the tables hold facts only.

### Token events

`explorer.token_events` (kept forever) is the history of a denom that the
chain state forgets, one row per denom named, `seq` counting from 0 per
transaction (or per block, `tx_index = -1`), `actor` the message's signer:

- from the tokenfactory messages of a successful transaction: `created`
  (`MsgCreateDenom`, the denom `factory/<creator>/<subdenom>`, details
  `subdenom`), `minted` and `burned` (`MsgMint`/`MsgBurn`, `amount`; a mint
  goes to its signer, details `recipient`), `admin_changed` (details
  `{"from", "to"}` from `DenomAdminChangeEvent`, `to` empty when renounced),
  `metadata_changed` (details symbol, name, display) and
  `branding_changed`;
- from the tradebin typed events: `market_created` (a row for the base and
  one for the quote, details market_id, base, quote), `pool_created` (base,
  quote and the pool's LP denom) and `halted`/`unhalted`
  (`DenomHaltedEvent`/`DenomUnhaltedEvent`, which governance enacts in
  EndBlock, so block-level rows without an actor).

Every denom a token event names is marked dirty for the denoms set, and so
are the tokenfactory change events.

### Governance

Proposals, votes and deposits are kept forever; the gov module deletes the
votes once it tallies a proposal, so the explorer is the only place keeping
them. From the events of a successful transaction:

- `MsgSubmitProposal` writes the `proposals` row: id and proposer from the
  `submit_proposal` event, title, summary, metadata, expedited and messages
  from the body (a v1beta1 submission's legacy content is wrapped in a
  `MsgExecLegacyContent`, as gov v1 queries show it), `kind` from the message
  types (the legacy content's type for a `MsgExecLegacyContent`; the first
  message with a kind wins, no message is `text`), status `voting_period`
  when the initial deposit opened the voting (`voting_period_start`), else
  `deposit_period`, and submit height, transaction and time. A row the sync
  wrote first only gets the submit fields it lacks.
- Every `proposal_deposit` event with a depositor is a `proposal_deposits`
  row (the initial deposit included; one depositor's deposits in one
  transaction are one row); `voting_period_start` moves the proposal out of
  its deposit period.
- Every `proposal_vote` event upserts the voter's `proposal_votes` row:
  `options` as SDK v0.50 encodes them (`[{"option":1,"weight":"1.0…"}]`;
  the pre-v0.47 text form is converted), `option` when one option holds the
  whole weight. A later vote replaces an earlier one; an earlier one written
  later (the backfill walks down) never does, except over a vote the
  snapshots command read from the archive (`source` archive), which gives way
  to its transaction.
- `cancel_proposal` sets status `canceled` (the chain deletes the proposal).

From the block's finalize events, `active_proposal` sets `passed`,
`rejected` or `failed` from `proposal_result` with `resolved_height`
(`expedited_proposal_rejected` puts an expedited proposal back into its
voting period as a regular one), and `inactive_proposal` sets `dropped`
(the deposit was not reached in time) or `failed`. Both are also `block_events`
rows. A resolution never goes back to an earlier one, and an open status
never undoes a resolution. Every proposal these events name is dirty for
the proposals set.

### Accounts

Every signer of every transaction gets an `explorer.accounts` row:
`first_seen_height`/`first_seen_time` move back with `LEAST`,
`last_seen_height` forward with `GREATEST`, and `tx_count` counts the
transactions the write actually inserted (the keys the `transactions`
insert returned), so writing a height again, or a reindex, never counts
twice. Addresses that only appear in activity rows get their row from
those, and `activity_count` counts the activity rows the write inserted.
Both writers merge
the deltas of a write per address and apply them in address order
(`mergeDeltas` in `internal/writer`, shared with the later per-token
totals), so concurrent writes lock the rows in the same order; a write that
still hits a deadlock (SQLSTATE 40P01) is retried once.

### Account activity

The transformer writes one `explorer.account_activity` row per address per
transaction (`internal/transform/activity.go`), never per message:

- **Addresses**: every `bze1…` value with a valid checksum in the
  transaction's event attributes (typed-event values decoded) and message
  bodies, plus the signers. Module accounts (the labels seed's list) and
  the ICS-20 escrow accounts of the channels the events name never get
  rows. A non-signer named only by relay messages (`MsgRecvPacket`,
  `MsgAcknowledgement`, `MsgTimeout`, `MsgTimeoutOnClose`,
  `MsgUpdateClient`) gets one only when its balance changed.
- **Deltas**: Σ `coin_received` − Σ `coin_spent` per denom, zero entries
  dropped; the fee is part of it.
- **Kind and category**: from the classification of the first message
  naming the address (the signer category for a signer, whose `msg_types`
  are all the transaction's, the participant category otherwise). A
  `MsgRecvPacket` receiver with a positive delta is `ibc_in`, a sender
  refunded by an acknowledgement or a timeout `ibc_refund`. An address named
  only by the ante handler's events (a fee granter) is `fee_only`. A failed
  transaction gives one `fee_only` row (category `other`) to its fee payer.
- **Counterparty**: the entry's `counterparty_attr` over the messages of the
  first message's type, when it names exactly one account or validator
  operator other than the address. When it names nobody else, a non-signer
  of a non-relay message gets the one sender of those messages (the
  recipient of a send gets the sender). A foreign address or a proposal id
  is no counterparty.

Block-level rows (`tx_index` -1, one per address per block) come from the
stored block events whose classification lists `address_attrs`: kind and
category of the first such event, `details` the events naming the address,
`deltas` from the block's `coin_spent`/`coin_received` events.

The insert returns the keys it wrote, so `activity_count` moves only for new
rows, and the reindex overwrites rows like any other table.
`explorer.reclassify_unknown()`, which `migrate up` runs, gives rows stored
as `other` the kind and category of their first message type once the Go
classification has an entry for it.

### DEX orders and fills

The order book works in two steps: a transaction only queues a message
(`OrderCreateMessageEvent`, `OrderCancelMessageEvent`; a `MsgFillOrders`
emits one create event per queued fill), and the engine processes the queue
at EndBlock, normally of the same block, emitting `OrderExecutedEvent` per
fill, `OrderSavedEvent` when a remainder rests on the book and
`OrderCanceledEvent`. Since chain v8.2.0 every one of these events carries
the queue message id (24 zero-filled digits, unique within a block).

- `explorer.order_messages`: one row per transaction-time event with a
  message id, kind `create`, `cancel` or `fill` (a create event under a
  `MsgFillOrders`).
- `explorer.orders`: one row per `OrderSavedEvent`. Its insert reads the
  fills and the cancel already stored, so an order written after them (the
  backfill walks down) starts with the right `filled_amount`, status and
  `closed_*`. A cancel sets `cancelled` and `closed_*`.
- `explorer.order_fills`: one row per `OrderExecutedEvent`, `seq` its
  block-event position. Only the fills the insert returned add to their
  order's `filled_amount` (merged per order, applied in id order); an order
  whose fills reach its amount is `filled`, closed at its last fill.
- Same-block attribution: the transformer holds a block's transactions and
  its EndBlock events together, so an outcome whose message id matches a
  message of the same height gets `orders.created_tx_hash` /
  `order_fills.taker_tx_hash`.
- Cross-block attribution: the engine processes at most 500 messages per
  block and keeps a message whose processing failed for a later block, so
  an outcome can land after its transaction. Both writers run the
  `writer.AttributeOrders` PostFlush hook: an unattributed outcome with a
  message id gets the transaction of the `order_messages` row with that id
  at the highest height at or below its own (a newer message cannot carry
  the id while the older one is queued, and the outcome block's own
  transactions carry higher ids). The counter restarts whenever the queue
  empties, almost every block, so the rule is only exact once every height
  from the message to the outcome is indexed: the hook links an outcome
  only then, and its candidates are every unattributed outcome at or above
  the flush's lowest height, so the flush that writes the message, the
  outcome or the last missing height in between makes the link, in either
  walking direction and whatever order the backfill's flushes land in.
  Both statements only fill NULL columns; a reindex keeps a link.
- Before chain v8.2.0 no event carries a message id. The transformer then
  matches each market's EndBlock outcomes to the same block's
  `OrderCreateMessageEvent`s and `MsgFillOrders` bodies in queue order (the
  order the transactions enqueued them; v8.0 runs the whole queue in that
  order, v8.1 market by market, the same within a market): a message claims
  the first run, at or after the previous match, of fills of the other side
  at its price taken by its owner (adding up to at most its amount), then
  for a create the order saved with the rest. It applies only to outcomes
  without a message id, so it needs no upgrade height (and works on the
  testnet too). It is approximate: a look-alike outcome of a message queued
  in an earlier block can be claimed, a message only refunded claims
  nothing, chain v7.0 and v7.1 events (below 15,951,000) name no owner,
  maker or taker (market, side, price and amount only); an outcome without
  a match stays unattributed and `message_id` stays null.
- Owners before v7.2.0: a matched order takes its message's creator as
  `owner`, a matched fill as `taker`. The writer names the `maker` of such
  an order's fills from its owner, whichever of the two is written first,
  and a reindex keeps an owner the state sync named. A reindex of
  12,940,553..15,950,999 fills the rows of a database written before.
- Reconciliation: the chain removes a remainder below the market minimum
  without an event; the hourly `orders` set (see the state sync) closes it.

A transaction's `dex.pending` is true while one of its create or fill
messages (chain v8.2.0 and later) has no attributed fill or order, or one of
its cancels names an order still open. It resolves when the cross-block
attribution links the outcome. Before chain v8.3.0 a fill message whose
orders were gone is refunded without any event, so it stays pending (the UI
treats a settled fill as final). Since v8.3.0 the engine emits
`OrderMessageRefundedEvent` for what it sends back, `OrderCancelSkippedEvent`
for a cancel that found no order and `OrderMessageFailedEvent` for every
block it fails to process a message (which stays queued). They are kept
only as `block_events` rows (index `idx_block_events_order_outcome` on the
message id and height, migration 000022) and linked to the transaction when
it is read: an event belongs to the transaction's order message with its
message id at or below the event's height when no later order message took
the id in between. A refund or a skip settles its message; a failure does
not.

### Open orders

`GET /api/v1/accounts/{address}/open-orders` answers from one of two
sources. `explorer.orders` is built from events only, so it holds every open
order only when the indexed history covers the DEX from its first market
(an order placed below the oldest indexed height and still open is missing;
the testnet never has that history).

- Coverage (`internal/ordercoverage`, a serve component): the first
  `MarketCreatedEvent` per chain id is hardcoded (`beezee-1`: 12,940,553);
  another chain id has none and always uses the node. The table counts as
  complete when every height from that start up to `last_indexed_height`
  has a `blocks` row and no unresolved `index_failures` row lies in that
  range. A verified watermark is kept in memory: the first check counts the
  whole range, then every minute only (watermark, last]. Blocks rows are
  never deleted, so the watermark only grows; a gap or an open failure
  makes it incomplete and the next check starts over. Requests never wait
  for it: before the first check, or after a database error, the node
  answers.
- Index path: one query over the owner's open rows (`idx_orders_open_owner`),
  remaining = `amount − filled_amount`, as of `last_indexed_height`. A
  remainder the chain drops below the market minimum without an event stays
  open until the hourly `orders` set closes it.
- Node path (`internal/openorders`): tradebin `UserMarketOrders` (every
  market, pages of 100), then `MarketOrder` per reference (8 at a time; a
  NotFound, filled or cancelled between the two reads, is skipped), every
  read pinned to the node's `/status` height. The local node's gRPC is asked
  first; when it cannot be reached (an RPC transport error, gRPC Unavailable
  or a timed-out call) the same queries go through the archive's
  `/abci_query` at its latest height. Both unreachable, or a node answering
  an error: 502.
- Markets (base and quote) come from tradebin `AllMarkets`, cached 5 minutes
  and reloaded once for a market id the list does not have; ids are looked
  up whole, never split on `/` (factory denoms contain slashes).

### Cross-chain transfers

A transfer's status comes from BZE's own events: the relayer that carries a
packet brings the other chain's acknowledgement, or the timeout, back to BZE
as a transaction. No other chain is ever queried. `explorer.ibc_transfers`
holds one row per BZE-side channel, packet sequence and direction (ICS-20
packets on port `transfer` only):

| Step | Events | Row |
| --- | --- | --- |
| `MsgTransfer` | `send_packet` (packet data from `packet_data_hex`) | `out`, `pending`; the denom is the BZE denom the packet's trace names |
| `MsgAcknowledgement` | `acknowledge_packet` + `fungible_token_packet` with `success` (the ack result, any value) | `delivered` |
| `MsgAcknowledgement` | `acknowledge_packet` + `fungible_token_packet` with `error` | `refunded_rejected`, `ack_error` |
| `MsgTimeout`, `MsgTimeoutOnClose` | `timeout_packet` | `refunded_timeout` |
| `MsgRecvPacket` | `recv_packet` + `fungible_token_packet` with `success = "true"` | `in`, `received` |
| `MsgRecvPacket` | `recv_packet` + `bze.txfeecollector.BlockedIbcInboundEvent` (chain v8.2.0) | `in`, `rejected`, `ack_error` "blocked inbound denom" (another error ack: its error) |

`timeout_height` `0-0` and `timeout_timestamp` 0 mean none (NULL). Every
final status sets `result_*`. The backfill walks down, so an
acknowledgement or a timeout can be written before its transfer: it carries
the whole packet in its message body and inserts a complete row (its own
transaction as `created_*`); the transfer's write then replaces `created_*`
and the timeouts and never touches the status or `result_*`. The settling
write changes only a `pending` row, so replays and backfill overlap are
idempotent, and both orders end with the same row. A transfer no relayer
ever settles stays `pending`, shown as `expired` past its timeout time.

### Stake movements

`internal/stakeflow` keeps the validators' stake movements of the last 7 days
in memory; nothing is stored for them. They come from the stored message events
(`explorer.messages.events`), not from type URLs, so authz `MsgExec` delegations
count too:

| event | movement |
| --- | --- |
| `delegate` | `delegate`, + on `validator` |
| `unbond` | `undelegate`, − on `validator` |
| `redelegate` | `redelegate_out` − on `source_validator`, `redelegate_in` + on `destination_validator` |
| `cancel_unbonding_delegation` | `cancel_unbonding`, + on `validator` |
| `create_validator` | `create`, + on `validator` (the delegator is the operator's account) |

Only bond denom amounts count; slashes are not movements (they are
`validator_events`). `serve` builds the set and runs it as a component: one
read-only, repeatable-read load at start (the indexed height spans from the
last block before the window, and the window's messages with one of these
events), then every flush of the live writer and of the backfill's batch writer
reaches it through a `PostFlush` hook (`stakeflow.FromEntities`, the same
movements the load reads back). Flushes seen before the load ends are applied
after it; a height already applied (in the load, or a write retried after a
rollback) counts once.

The window is anchored on the newest indexed block's time, never on a clock:
each new block moves it, a movement leaves the 24-hour total once older than a
day and the set once older than 7 days. Each validator's two totals are running
sums, adjusted as movements arrive and leave. `complete` is true once every
height from before the window's start up to the newest is indexed: a fresh
explorer younger than 7 days, a backfill still inside the window or a catch-up
gap all make it false. The set is a few MB even on a busy week.

## Reindex

`bze-scan reindex` runs chosen heights through the backfill pipeline again
and overwrites what is there: the repair tool after an outage, for the
heights listed in `index_failures`, and after a transformer fix that changes
what a height produces. It needs the same `DATABASE_URL`, `NODE_RPC_URL`,
`ARCHIVE_RPC_URL` and `ARCHIVE_RPC_RETRY_URL` as `serve`, and takes its
tuning from the `BACKFILL_*` variables (`BACKFILL_ENABLED` and
`BACKFILL_FLOOR` are ignored).

```
bze-scan reindex --heights 24998319,24998402      # a list
bze-scan reindex --from 24990000 --to 24998000    # a range, both included
bze-scan reindex --failed [--source live|backfill|reindex]
                                                  # every open index_failures row
bze-scan reindex --failed --dry-run               # print the selection, exit
bze-scan reindex --from 1 --to 100 --workers 4    # override BACKFILL_WORKERS
```

Exactly one selector is allowed. Heights are dispatched highest first. The
command prints `reindex done heights=N failed=M duration=…` on stdout, lists
each failed height on stderr, and exits 0 when every height succeeded, 2 when
some failed, 1 on a configuration or database error (or an interrupt).

- **Source per height.** The local node when the height is at or above its
  `earliest_block_height` (read once from its `/status`), the archive below
  it, with the retries on `ARCHIVE_RPC_RETRY_URL`. A reindex keeps short
  retries (0.5 s, 1 s, 2 s), not the backfill's backoff: it is run by hand
  and watched, and a height that still fails can be reindexed again. A
  height the local node pruned during the run goes to the archive too; when
  the local node does not answer `/status`, every height does.
  `BACKFILL_RATE_LIMIT` paces all requests, local ones included. Old heights
  pass through the archive adapter as in the backfill.
- **Update mode.** Every table is written by an `UPDATE` of the rows whose
  values differ, then the `INSERT … ON CONFLICT DO NOTHING RETURNING <key>`
  of the backfill. The rows the insert returns are exactly the rows this
  write inserted, also when another writer inserted the same key
  concurrently. They reach the `PostFlush` hooks as `Flush.Inserted`, and any
  counter or total moves only for them, so a reindex never counts twice.
  (`RETURNING (xmax = 0)` cannot tell an insert from an update here:
  PostgreSQL refuses system columns on partitioned tables.) Identical rows
  are left alone, so reindexing a healthy range rewrites nothing.
  `block_time_ms` is recomputed for the flushed heights and the one above.
  A table added by a later story joins the update path through its spec in
  `internal/writer/tables.go`, and its tests get a reindex case.
- **Failures.** A height written resolves its open `index_failures` rows
  (`resolved_at = now()`, every source); a height that fails after its
  retries is upserted with source `reindex`.
- **Checkpoint.** A `backfill_checkpoints` row named
  `reindex-<RFC 3339 start time>` records the range, `lowest_dispatched`
  and `blocks_done` after every flush, and ends `done`, or `error` with the
  cause after a database error or an interrupt. A reindex does not resume:
  run it again, it is idempotent.
- **Beside `serve`.** Nothing stops both from writing the same height, and
  idempotent writes make them converge on the same rows, so reindexing the
  live head while `serve` runs is pointless but harmless. No state resync is
  triggered and the raw-JSON cache is not touched. The node's sink tables are
  never written.

## State sync

The current-state tables come from the local node's gRPC
(`NODE_GRPC_ADDR`), never from a public node. Inside `serve` (with the
indexer) the state sync:

1. resyncs every registered set in full at start, in registration order, so
   a resync lost at shutdown is harmless;
2. resyncs what each live block changed: the transformer fills the block's
   dirty set (validators, proposals, denoms, channels, params; a passed
   proposal marks params) and the live indexer publishes it after the
   write. The backfill and the reindex ignore it: history cannot change
   current state. Keys go to an in-memory queue served by two workers; a
   key already queued is not queued twice, and a queued full resync absorbs
   the keys of its set;
3. resyncs a set in full when none ran for its interval (validators,
   proposals, proposals' non-voters, upgrade recovery, prices, chain state: one minute; denoms, holders, IBC
   channels, orders: one hour;
   chain registry, params: one day; daily statistics: every night at
   00:10 UTC), the safety net for what no event announces. A full resync, from
   a block or the timer, restarts the wait.

Every run is recorded in `explorer.sync_jobs` (`last_run_at`,
`last_success_at`, `last_error`, `cursor`). A node that is down fails the
run: it is logged and recorded (the prices set logs a failing aggregator once
per change of state, not every minute), the rows stay as they were, and the next
trigger retries. Nothing here is on the user's critical path. `bze-scan
sync-state` runs the full resync of every set once and exits 1 when any
failed; `--job <set>` runs one set only, `--job <set> --id <key>` one entry
of it.

**Validators.** A full resync reads staking `Validators` (every status, 200
per page), slashing `SigningInfos` and `Params`, and per validator staking
`Delegation(owner, validator)` (the self-delegation, 0 when there is none)
and `ValidatorDelegations` with `count_total` (the delegator count). The
owner `bze1…` is the operator's key bytes; `consensus_address` is the
upper-case hex address of the consensus key, as `/block` names the proposer.
Ranks and `voting_power_pct` (five decimals, half up) are over the bonded
tokens; other statuses are unranked. A validator the node no longer lists is
kept with `status = unbonded`. Each owner gets a `labels` row
(`validator_owner`, named after the moniker, `source = sync`; rows of another
source are never overwritten). A dirty validator is resynced alone (staking
`Validator`, slashing `SigningInfo`) and every bonded validator is re-ranked
over the stored standings.

A validator's `logo_url` is the Keybase picture of its description
`identity` (a Keybase key suffix): `GET KEYBASE_URL?key_suffix=<identity>&fields=pictures`,
`them[0].pictures.primary.url`, as the BZE staking dapp does. Both resyncs
look a validator up, one after the other, only when its `logo_checked_at` is
null, older than 24 hours, or its identity changed; a validator without an
identity has a NULL logo. An identity that is not a key suffix (16 hex
digits; validators set placeholders such as `0` or `URI_VALIDATOR`) is never
sent and counts as a lookup with no picture, as does Keybase's `INPUT_ERROR`
answer. A successful lookup writes the URL (NULL when the
user has no picture) and `logo_checked_at`. A failed one is logged and never
fails the sync: the stored logo stays and `logo_checked_at` is not bumped
(after an identity change it is cleared, so the next run retries). The
validator upsert never writes the logo columns.

A block marks a validator dirty on `delegate`, `unbond`, `redelegate` (both
validators), `create_validator` and `cancel_unbonding_delegation` events and
on `MsgEditValidator` / `MsgUnjail` by operator address, and on `slash` and
`liveness` events and every entry of the block's `validator_updates` by
consensus address (key `cons:<HEX>`: the transformer has no table to look
the operator up in). The validators set folds a consensus key into the
operator it learnt from its last resyncs before queueing
(`statesync.Canonicaliser`), so a validator marked both ways is resynced
once; keys published before the start resync are folded again after it. A
consensus key of a validator the set has never seen runs a full resync;
an unreadable address or key asks for one too.

**Denoms.** A full resync reads bank `TotalSupply` and `DenomsMetadata`
(1,000 per page; a denom is listed when it has either), tradebin `AllMarkets`
`HaltedDenoms` and `AllLiquidityPools`, and per factory denom tokenfactory
`DenomAuthority`.
Supply and metadata always come from the node, never from events. `kind` is
the denom's shape: `native` for `ubze`, `factory` for `factory/…`, `ibc` for
`ibc/…`, `lp` for tradebin pool shares (`ulp/<hash>` since chain v8.2.0,
`ulp_<base>_<quote>` before), else `unknown`. `symbol`, `name`,
`description` and `logo_url` (the metadata `uri`) come from the bank
metadata and `exponent` is its display unit's (0 when the display unit is not
listed, as in the placeholder metadata ibc-go writes for every IBC voucher);
`ubze` has
no bank metadata and takes the chain's constants (BZE, exponent 6), and a
pool share's exponent is 12 whatever its metadata says (6), as the BZE apps
show pool shares (`LP_ASSETS_DECIMALS`). A factory
denom's `creator` is the address in it and `admin` the tokenfactory admin
(NULL once renounced). `markets` are the tradebin market ids (`base/quote`)
the denom trades in; `halted` is the tradebin halt (a node before v8.2.0
answers `HaltedDenoms` with Unimplemented: nothing is halted). First sight
(`created_*`) is the denom's earliest `created` token event, filled at the
next resync once the event is indexed. An LP denom's `lp_base`/`lp_quote`
are its pool's assets, from the pool whose stored `lp_denom` it is (a
`ulp/<hash>` denom is the SHA-256 of the pool id and cannot be reversed);
only a legacy `ulp_<base>_<quote>` missing from the pool list falls back to
splitting it on `_` (exactly two assets). The LP row is then named after the
assets' symbols as resolved in the same run, `<BASE>/<QUOTE> LP` and
`<BASE>/<QUOTE> LP Shares` like the BZE dapps (an LP resynced alone resolves
its two assets for that, without saving them); when a side has no symbol the
row keeps the bank's. A node without the AMM (`AllLiquidityPools`
Unimplemented) leaves only the fallback. A denom the bank no longer lists keeps
its row with a zero supply. The holders and prices columns are left to their
own jobs.

A native or Factory denom whose bank metadata has no logo (none has one on
mainnet) takes the logo of its asset in the registry of the explorer's own
chain (`CHAIN_ID`), when the registry lists it.

An IBC denom is traced with transfer `DenomTrace` (`ibc_path`,
`ibc_base_denom`). Its `origin_chain_id` is the counterparty chain of the
path's first channel in `explorer.ibc_channels` (null until the IBC channels
sync fills it). Its symbol, name, exponent and logo then come from the
registry asset `(origin chain, base denom)` and win over the bank metadata,
which for a voucher is ibc-go's placeholder (`UUSDC`, exponent 0). A
multi-hop denom (`transfer/channel-0/transfer/channel-94814/uatone`) is
followed through the first hop chain's asset for the rest of the path
(`ibc/` + SHA-256 of it) and its earliest IBC trace to the home chain, which
becomes the origin; when that fails the first hop's chain and the raw base
denom stay. A multi-hop denom keeps its first hop's chain as `via_chain_id`
(null for one hop), so two vouchers of one asset arriving by different
routes can be told apart. A chain or an asset the registry cache misses is published to
the chain_registry set (by chain id, or `name:<registry name>` for a trace's
chain).

**IBC channels.** Registered first, since the chain registry names their
counterparties and the denoms take their origin from them. A full resync
reads the channel `Channels` query (200 per page) and, per channel on port
`transfer`, `ChannelClientState`: the Tendermint light client's `chain_id`
is the counterparty chain (another client type names none), then the
client's `ClientStatus` (`active`, `expired`, `frozen`, stored lower case;
a failed query keeps the stored one). It upserts
`explorer.ibc_channels` (a counterparty the node did not answer keeps the
stored one), one `ibc_escrow` label per channel (`source = sync`, module
`transfer`, the address `transfertypes.GetEscrowAddress` derives, named
"Cross-chain escrow (<pretty name, else chain id>)"), and fills
`ibc_transfers.counterparty_chain_id` where the transfer was stored before
its channel was known. A counterparty absent from `explorer.chains` goes to
the chain registry set at once. Channel open and close events mark the set;
the chain registry marks it after a run that saved a chain, so the escrow
names follow the registry. No event marks a client expiring: the hourly
run is what notices it. A denom whose first channel is closed or whose
client expired or froze, or whose origin or via chain the registry lists
as `killed`, is `stranded` on the tokens API.

**Chain registry.** `explorer.chains` and `explorer.registry_assets` cache
the Cosmos chain registry: the GitHub contents API lists its directories
(mainnets and `testnets/`, `CHAIN_REGISTRY_API_URL`), each chain's
`chain.json` and `assetlist.json` come from `raw.githubusercontent.com`
(`CHAIN_REGISTRY_RAW_URL`, no API rate limit). The set refetches BZE
(`CHAIN_ID`) and every chain the explorer met (channel counterparties,
denom origins, chains already cached) daily, and a missed chain at once with
a 10-minute cool-down per key. The registry is organised by directory: a
chain id is found by reading `chain.json` files, the directory named like the
chain id first (`cosmoshub` for `cosmoshub-4`); every one read is
remembered until the next listing (daily), so the whole registry is read at
most once a day. A chain id no directory holds gets a `chains` row with
`registry_fetched_at` NULL, shown by its id. A chain row holds the pretty
name, network type, bech32 prefix, logo (PNG, else SVG) and the first
explorer's `tx_page`/`account_page` templates; an asset its symbol, name,
display unit and its exponent, logo, CoinGecko id and `traces`. A failed
fetch keeps the previous rows; `updated_at` moves only when a value changes.

**Holders.** `explorer.token_holders` is a snapshot of bank `DenomOwners`,
never a balance derived from events: at start and hourly every denom is paged
(1,000 owners a page, 200 ms between pages, one denom after the other), each
page upserted with the run's stamp; after the last page the denom's rows the
run did not stamp are deleted and `holders_count` is recomputed (owners of at
least `10^exponent` base units) in one transaction. The job's cursor in
`sync_jobs` is the denom, page key and stamp being worked on, so a restart
resumes at the failed page; `{}` once a run finished.

**Orders.** At start and hourly the open orders are reconciled with the
node's order book, a safety net for the remainders the chain removes without
an event. The tradebin queries list an owner's orders, not a market's book,
so a run reads tradebin `UserMarketOrders` of every owner with open rows
(every market at once, 1,000 references a page), every read pinned to one
height: the live cursor, or the node's latest height when lower or when the
live indexer has not written. An open row created at or below that height
that the node no longer lists is closed as `filled` with `closed_height` /
`closed_time` that height and its block time (locked in order id order like
the writers). It never reopens a row and never touches a row the node still
lists; a later fill or cancel event still applies to a row it closed. An
owner whose read fails keeps its rows and fails the run. An open row without
owner (chain v7.0/v7.1, its message not matched) is read with tradebin
`MarketOrder` (market, side, id): the node's owner names the row and the
maker of its fills, a not-found closes it like the others, a failed read
keeps it and fails the run.

**Prices.** Every minute the CoinGecko feed behind `/api/prices` (see
"Aggregator-compatible endpoints") gives USD prices by CoinGecko id
(`bzedge`, `cosmos`, …), with or without an aggregator (testnet has none). A
denom takes the price of its registry asset's CoinGecko id: BZE's own asset
list first (`ubze` is `bzedge`, and it lists the IBC denoms BZE holds), else
the origin chain's asset. A denom no longer priced is cleared; a failed fetch
keeps every price. The explorer computes no DEX price. With `AGGREGATOR_URL`
set, `ubze`'s `price_change_24h_pct` comes from the aggregator's
`/api/dex/tickers`, read at
most every 5 minutes: the ticker of `PRICE_CHANGE_MARKET` (the BZE/USDC.inj
liquidity pool, which prices USDC.inj in ubze), as last over open when `ubze`
is the market's base and open over last when it is the quote, in percent
with four decimals. A market the aggregator no longer lists clears it, a
failed read keeps it, a market that does not trade `ubze` is an error. Every
other denom's change stays null.

A dirty denom (a token event, a tokenfactory change event) is resynced alone
(bank `DenomMetadataByQueryString` and `SupplyOf`, the factory admin, the
markets and halts). Every denom a block moves is also marked "seen"
(`seen:<denom>`): the set drops the key when it already holds the denom and
resyncs it otherwise (`statesync.Canonicaliser` folding to an empty key), so
a new IBC voucher appears within its first block without every `ubze` move
costing a resync. Before its first full resync the set knows nothing yet and
keeps the seen keys for the fold after it.

**Proposals.** The set's first run lists every proposal (gov `Proposals`,
100 per page), so one submitted before the live floor exists too; every
minute after, it refreshes the proposals in their voting period (the node's
list of them plus the stored ones it no longer lists). A dirty proposal is
resynced alone (gov `Proposal`). Title, summary, metadata, status, times,
messages and `total_deposit` come from the node. In the voting period the
tally is the node's running `TallyResult`, measured against the staking
pool's bonded tokens (`tally_bonded_tokens`, for turnout, with `tally_source`
`sync`), `tally_updated_at` the sync's time; once resolved it is the proposal's final tally, as of its
voting end, and the last bonded tokens are kept. An answer read before a
resolution was indexed never overwrites the resolved status or tally. A
proposal the node no longer has (dropped, canceled) keeps the row its events
wrote. A proposal the sync writes before its resolution is indexed takes
`resolved_height` from the stored `active_proposal`/`inactive_proposal`
block event.

**Proposals' non-voters.** The `proposal_non_voters` set records, for every
proposal whose voting ended in a tally (passed, rejected, failed; never a
canceled or dropped one), the validators of the set the tally counted that
did not vote, and only those: `explorer.proposal_non_voters` (consensus
address, operator address, consensus power, share of the set's power with
five decimals) plus `active_set_height`, `active_set_size` and
`non_voters_at` on the proposal. Every minute it computes the proposals
with a `resolved_height` (`H`) and no `non_voters_at` whose votes are
complete: every height from `submit_height` to `H` indexed (no vote comes
before the submission), or the votes read by the snapshots command
(`tally_source = 'archive'`; a proposal submitted below the indexed range
waits for it). The rows and the columns are written in one transaction.

- The tally set. Gov tallies in its EndBlocker at `H`, over the staking
  power index. CometBFT applies the validator updates of EndBlock(N) at
  N+2, so the set is CometBFT's `/validators` (every page of 100) at `H+1`
  where gov's EndBlocker runs before staking's (v5.0, v8.0.0 on) and at
  `H+2` where staking's runs first (v5.1.2 to v7.2.0; the per-generation
  rule is the chain's `archive.Chain.TallySetHeight`). The validators slashed in `H`'s block
  (its `slash` events) are left out: the slashing BeginBlocker jailed them,
  which drops them from the power index before the tally, while CometBFT
  still lists them. The set is never recomputed from staking state.
- A member voted when the account of its operator (the same key bytes under
  `bze`) has a `proposal_votes` row. Operators come from the validators
  table, else from the indexed `MsgCreateValidator` of a validator staking
  deleted since (its consensus key); an address neither knows is stored
  without an operator, and logged.
- The set and the tally's block are read from the local node, or from the
  archive (`ARCHIVE_RPC_URL`) once the local node pruned the height, so a
  proposal tallied a minute ago and one of years ago go the same way.
- Cross-check: the set's power × 10^6 must match `tally_bonded_tokens` within
  one unit of power per validator (each truncates its tokens); a mismatch
  is logged as a warning, never fails the run.

A written proposal is not computed again; `sync-state --job
proposal_non_voters --id N` recomputes it (replacing its rows) once its votes
are complete, and fails before.

**Upgrade recovery.** When each validator came back after a software
upgrade halted the chain (display only, nothing scores it):
`explorer.upgrades` (one row per passed upgrade, by plan height `U`) and
`explorer.upgrade_recovery` (one row per member of the validator set at
`U+1`).

- The halt sits at `U+1`, not `U`: the old binary decides block `U` and
  stops while executing it (the upgrade module's PreBlocker panics), so
  consensus waits at `U+1` until two thirds of the power run the new binary.
  Block `U+1`'s header carries the time `U` was decided (`halt_time`), block
  `U+2`'s the time `U+1` was decided (`resume_time`). Mainnet v8.1.0
  (`U` 22,551,810) and v8.1.1 (23,855,000) both show their 12- and 2-minute
  gaps between `U+1` and `U+2`.
- The halt watch (a `serve` component next to the state sync,
  `HALT_WATCH_ENABLED`) polls the local node every `HALT_WATCH_INTERVAL`. It
  arms when the node's latest block is `U-1` or `U` of a pending upgrade and
  older than 30 s (not while catching up), reads the set at `U+1`, then reads
  `/consensus_state`: a validator back on the new binary votes at `U+1`
  round after round, and the first poll that sees it writes `back_time` (the
  poll) and `vote_time` (its earliest vote's own time), `back_source =
  'halt_vote'`, at once (a restart keeps them), and marks the upgrade
  `watched`. A vote's index is its position in the set (power descending,
  then address); its printed address prefix must match, else it is skipped.
  While the node does not answer (Cosmovisor swapping binaries) it polls on
  and records nothing; it disarms once the node is past `U`.
- The `upgrade_recovery` set (every minute) adds the passed upgrades
  (the plan of a `MsgSoftwareUpgrade`, or of a legacy content), and reads,
  once blocks `U+1` and `U+2` are indexed (a historical upgrade waits for the
  backfill), `/commit` from `U+1` on through the local node or the archive,
  paced by `BACKFILL_RATE_LIMIT`, at most 1,000 heights a run across every
  upgrade and never above the live cursor minus one. A member without a halt
  vote is back when the first block it signed was decided (the next block's
  time; `back_source = 'commit'`, `first_signed_height`, `blocks_needed`
  after `U+1`): those that signed `U+1` are back at the resume. An upgrade is
  computed (`computed_at`) once every member is back or the window
  (`UPGRADE_RECOVERY_WINDOW` heights from `U+1`) is read; a member still
  missing keeps no `back_time`. `seconds_back` is `back_time` minus
  `halt_time`, never negative. A halt vote is never replaced;
  `sync-state --job upgrade_recovery --id U` reads the commits again.

**Parameters.** The params set snapshots the parameters of every module the
parameters page shows (`internal/params`: txfeecollector, staking, mint,
distribution, slashing, gov, tradebin, tokenfactory, rewards, burner,
cointrunk) at start, daily and after every passed proposal (the
transformer marks `params` with the proposal id on an `active_proposal`
whose result is `proposal_passed`), reading them at the height `/status`
reports. A module whose JSON equals its latest snapshot writes nothing;
otherwise a `param_snapshots` row with `changed_keys`, the top-level keys
that differ (`{}` for a module's first snapshot). Upgrade handlers change
parameters without any message, so diffing is the only general method. The
change is attributed (`proposal_id`) to the proposal that triggered the run
when it is a passed `parameter_change` or `software_upgrade`, else to the
passed software upgrade whose plan height lies between the module's
previous snapshot and this one (an upgrade's handler runs at its plan
height, after the proposal passed), else to none. A module the node fails
to answer is skipped and the others are written. The rows only explain
changes: `/api/v1/params` shows the live values.

**Chain state.** Every minute `explorer.chain_state` gets one row per key,
overwritten in place with the height the node answered at (every gRPC read
of a run is pinned to the height `/status` reports, so one run's numbers
agree): `staking_pool` (bonded and not-bonded tokens), `supply` (`ubze`),
`mint` (inflation, annual provisions, mint params), `community_pool` (the
pool's coins, decimals as the chain keeps them), `chain_params`
(`max_validators`, `unbonding_time_s`, `community_tax`, from the staking and
distribution params), `validator_counts` (bonded, jailed, total from
`validators`), `price_ubze` (`price_usd` and `price_change_24h_pct` copied
from `denoms`, null until the prices job) and `node_status` (moniker,
network, latest height and time, catching up). A key that fails keeps its
previous row; a node whose `/status` fails writes nothing. It runs last at
start, after the validators and prices it copies. `/api/v1/stats` and the
validators header read these rows, never the node.

**Daily statistics.** `explorer.daily_stats` holds one row per closed UTC
day, aggregated from the indexed tables; the indexer never touches it (one
hot row per day would serialise every flush and break idempotency). Day
boundaries are `blocks.time` in UTC: the day's blocks are found on their
time, every other table is read on the height range they span. A row holds
`blocks`, `txs` (the blocks' `tx_count`, failed included), `txs_failed`,
`active_addresses` (distinct `account_activity.address`), `new_addresses`
(`accounts.first_seen_time` that day), `transfers` (rows, block-level
included), `fees` (the transactions' fees summed per denom, sorted by denom)
and `avg_block_time_ms` (null when no block of the day has one). At start
and every night at 00:10 UTC the set recomputes every closed day since the
oldest indexed block's day that has no row, the last three closed days
(late live corrections), and the days of the heights the backfill wrote
since the previous run: the job's `cursor` keeps the main job's
`lowest_dispatched` the last successful run saw, and the days from the
lowest indexed block at or above the new value to the highest at or below
the old one are recomputed, plus the day above for the heights still in
flight. Each day is an `INSERT … ON CONFLICT (day) DO UPDATE` of the whole
row, so any run can be repeated; a day with no indexed block gets no row
(the series shows a gap, never a zero). `sync-state --job daily_stats --from
YYYY-MM-DD --to YYYY-MM-DD` recomputes a range of closed days on demand.

**Validator events** (`explorer.validator_events`, kept forever) come from
two writers:

- the transformer: `created` (`MsgCreateValidator`; details moniker,
  commission rate, self bond), `description_changed` (the fields of
  `MsgEditValidator` that are not `[do-not-modify]`), `commission_changed`
  (`{"from", "to"}`: the message carries the new rate only, so the writer
  takes `from` from the stored `validators.commission_rate` when it inserts
  the row — exact on the live path, which writes before the sync updates the
  rate, approximate for history; a reindex keeps the stored `from`),
  `unjailed` (`MsgUnjail`) and `slashed` (the block's `slash` event,
  `tx_index = -1`; details reason, power, burned, consensus_address). The
  slash names a consensus address only: the writer resolves the operator
  through `validators.consensus_address`, and when no validator is synced yet
  the row keeps an empty operator until the next sync fills it in;
- the sync: `jailed`, `tombstoned`, `bonded` and `unbonded`, which no SDK
  event announces, when a resync finds the flag or the status (into or out of
  `bonded`) changed against the stored row. They are written at the live
  cursor's height (with that block's time), `tx_index = -1`, `seq` from
  10,000 so they never meet the height's slash events; nothing is written
  before the indexer has a cursor. Unjailing is the message's, not the
  sync's.

`first_seen_height`/`first_seen_time` are the earliest `created` event, else
the first sync; a `created` event indexed later (by the backfill) moves them
back.

**`blocks.signatures_power_pct`** is the share of the bonded validators'
tokens whose validator signed the commit, computed by the writers from the
commit's consensus addresses against the current `validators` table: exact
for a live block, approximate for history (the voting power at a past height
is not in `/commit`), NULL before the first sync.

## Snapshots

Some values are state as of a past height that the chain no longer has and
no event carries: the gov module deletes a proposal's votes and deposits
once it tallies it, and the turnout needs the bonded tokens at that moment.
The live path keeps them for the proposals voted while the explorer runs;
`bze-scan snapshots` reads them for the others from the archive node
(`ARCHIVE_RPC_URL`) through `/abci_query` at the height they describe, every
request paced by the backfill's limiter settings (`BACKFILL_RATE_LIMIT`).

It writes over what the database holds and never deletes a row, so running
it again writes the same values, and after a fix one more run repairs
history. It is run by hand (`docker compose run --rm backend snapshots`),
never by `serve`. Each kind lists its targets and snapshots one target in
one transaction; targets run four at a time (`--workers`), each with one log
line, and a failed one is logged, listed on stderr and leaves the others
going. Every run is recorded in `sync_jobs` as `snapshot:<kind>` with its
error. `--kind` and `--id` (both repeatable) narrow the run.

**Proposals.** Every stored proposal passed, rejected or failed after a
voting period (one dropped in its deposit period or canceled has nothing to
read). `H` is its `resolved_height`; when null (the explorer never indexed
the resolution), `H` is the first block whose time is at or after
`voting_end_time`, by binary search over the archive's `/block` headers from
the submit height (or the archive's earliest), and that block must carry the
proposal's `active_proposal` event; `H` is then stored. At `H - 1`, the last
state that still has them:

- the bonded tokens: the ubze balance of the `bonded_tokens_pool` module
  account (bank `Balance`; staking `Pool` fails at pre-v8 heights through the
  v8 binary, and the two are equal at every height), written over
  `tally_bonded_tokens` with `tally_source = 'archive'`;
- the votes (gov v1 `Votes`, every page): a stored vote gets its options
  rewritten in the events' encoding and keeps its height, `tx_index` and
  time; a missing voter gets a row at `H - 1` with `tx_index = -1`, the
  block's time and `source = 'archive'`; a stored voter the state does not
  list is left alone;
- the deposits (gov v1 `Deposits`): a depositor without a row gets one with
  its total (`H - 1`, `tx_index = -1`, `source = 'archive'`); depositors with
  event rows are left alone, since the state holds a total and the rows are
  per transaction. When the backfill later indexes the deposit transactions,
  the API leaves the archive total out.

The API marks these values historical through `tally_source` and `source`.

**Adding a kind**: implement `snapshots.Kind` (`Name`, `Targets(ctx, ids)`,
`Snapshot(ctx, reader, target)` reading through the `Reader` and writing in
one transaction, overwriting and never deleting) in a package under
`internal/snapshots/`, register it in `serve.NewSnapshots`, and record its
archive answers with `make fixtures ABCI=…` (or a new set there).

## Sink cleanup

The CometBFT psql sink (`public.blocks`, `tx_results`, `events`,
`attributes`) keeps every block the node writes; it has no retention of its
own. The explorer reads nothing from it but the notification of its trigger
on `public.blocks` (everything else comes from the node's RPC), so the sink
only has to keep what the live indexer may still be working on.

With `SINK_RETENTION` set, `serve` runs a cleanup at start and then every
`SINK_CLEANUP_INTERVAL` (`internal/sink`). The cut-off is the lower of:

- the newest height whose `blocks.created_at` is older than the retention;
- the live indexer's cursor (`indexer_state.last_indexed_height`) minus 1000.

Every height below the cut-off is deleted; the cut-off height itself is kept.
Before the live indexer has written, on an empty sink, or with no block that
old, nothing is deleted. Heights go in batches of `SINK_CLEANUP_BATCH` from
the oldest stored one up (each batch starts at the oldest height left, so
gaps cost nothing), one short transaction per batch so the node's inserts
never wait long. Each transaction deletes children first: `attributes` (of
the batch's events), `events`, `tx_results`, then `blocks`, which works with
and without `ON DELETE CASCADE` on the sink's foreign keys. Each run logs one
line with the cut-off and the rows deleted per table; an error is logged and
the next run starts where this one stopped. `bze-scan sink-cleanup` runs one
pass by hand.

It is **off by default** and must stay off while another reader of the sink
still needs its rows: the aggregator's listener reads its unprocessed swap
events from there, and its own cleanup crons trim the sink meanwhile. The
database role needs `SELECT` and `DELETE` on the four sink tables (the
explorer otherwise holds `SELECT` and `TRIGGER` on `public.blocks` only).

The stock schema has no index on `events.block_id`, so each batch's events
delete scans `events`; with a daily run over a sink trimmed to a few days
that stays short, and an index on `events(block_id)` removes it.

## Test fixtures and the fake node

`internal/testutil/fakenode` serves the URI form of the by-height RPC routes
(`/status`, `/block?height=N`, `/block_results?height=N`, `/commit?height=N`,
`/validators?height=N&page=P&per_page=100` from
`testdata/<N>/validators.<P>.json`, recorded at per_page=100 only)
and `/abci_query` at a height from recorded mainnet responses, byte for
byte. An `/abci_query` is keyed by height, method and request bytes
(`testdata/abci/<height>/<svc>.<Method>.<hex>.json`); one without a recording
answers code 1 ("no fixture"). A height may hold only some of its three
files; `FixtureHeights` lists those recorded in full. A height without a fixture
answers the JSON-RPC error a real node returns above its tip (HTTP 500,
`testdata/above_tip.json`). Tests can override the `/status` height, make the
node pruned below a height (`SetEarliestHeight`: `/status` reports it and
lower heights answer the node's "not available" error), answer a height's
seen commit (`SetSeenCommit(h, absent)`: `"canonical": false` and the last
`absent` committed signatures turned absent, until `ClearSeenCommit(h)`),
and read request counters per route and per height.

Fixtures are committed and re-recorded only on purpose, when a test needs a
height the existing ones do not cover:

```
make fixtures HEIGHTS="24998316 24998321..24998330" ARCHIVE_RPC=https://rpc.getbze.com
```

`BLOCKS="…"` records `/block` alone; `ABCI="43@17131437 47@23821225"`
records, per proposal id and resolution height `H`, the snapshots command's
reads at `H - 1` (bank `Balance` of the bonded pool, gov v1 `Votes` and
`Deposits`, their requests encoded by hand in the script), `/block` at
`H - 1` and `/block_results` at `H`. Recorded for proposals 43 (27 votes;
the 25 `/block` heights its resolution search visits from the archive's
earliest block to the recorded tip are `BLOCKS`) and 47 (24 votes).
`VALSETS="17131439 23821226"` records every page of `/validators` at those
heights: the sets proposals 43 and 47's tallies counted (31 and 23
validators). `VALSETS` is not `VALIDATORS`, the gRPC recorder's operators.
`COMMITS="22551811..22551816"` records `/commit` alone (with `VALSETS=22551811`
and the blocks 22551811 and 22551812 in `HEIGHTS`: the v8.1.0 upgrade's halt
and resume, for the upgrade recovery). `CONSENSUS_STATE=1` records
`/consensus_state` (asked until an answer holds at least eight votes) into
`testdata/consensus_state.json`, which the fake node serves until
`SetConsensusState` replaces it (`ConsensusStateBody` and `Vote` write a
round in the node's format).

Recorded today: 24998316 to 24998330 except 24998319, which is left out on
purpose: the fake node answers it with the above-tip error, which the live
indexer and backfill tests use as a failing height between good ones. The
backfill tests walk that whole range (24998316, 24998321 and 24998326 carry
tradebin transactions). Heights with
transactions: 24999004 (multi-message `MsgCancelOrder` transactions and an
out-of-gas `MsgCreateOrder`), 24999134 (multi-message `MsgCreateOrder`),
24999205 (IBC `MsgTransfer` over channel-3, sequence 5437), 24999209
(relayer `MsgUpdateClient` with `MsgRecvPacket` / `MsgAcknowledgement`, the
latter acknowledging 24999205's packet), 25000439 (`MsgWithdrawDelegatorReward`
+ `MsgWithdrawValidatorCommission`), 25000440 (authz `MsgExec`) and 25000894
(`MsgSend`). Validator events: 22933748 (`MsgEditValidator` raising a
commission), 24113494 (`MsgCreateValidator`), 24129272 (`MsgUnjail`),
24151894 (`MsgEditValidator` of a description) and 24160001 (a downtime
`slash`, its `liveness` event and the validator update removing it from the
active set). Transfers and block events come from the same heights: the fills
of 24999134 (settlements at block level, `OrderExecutedEvent`), the failed
transaction of 24999004 (its fee row only), the slash of 24160001 (a burn) and
the empty 24998317. Cross-chain transfers: those two, and from the old
generations a send and its acknowledgement (4002550, 4002556), an inbound
transfer (10002143), a send nothing settles (20235036) and two timeouts
(20237220). No recorded height has a rejected acknowledgement or a blocked
inbound transfer (the block arrived with chain v8.2.0, mainnet runs v8.1.1):
the unit tests build them from 24999209's recorded relay. Block-level
activity: 20267245 (a raffle win), 20329219 (a raffle loss), 20342600 (a
raffle's end: `RaffleFinishedEvent` and the burn of the rest of its pot; also
a v8.0.x DEX fill), 20300028 (v8.0.x DEX queue: an order saved and two
fills), all four with the doubled events of v8.0.x; 24844265
(`complete_redelegation`), 24871232 (`complete_unbonding`), 25006237 (the
four-weekly burn: `CoinsBurnedEvent`, two burn rows, day/hour/week epochs),
25035768 (`StakingRewardDistributionEvent` on the day epoch) and 25041911 (an
hour epoch alone). Daily statistics: 25003529 and 25003530, the last block of
2026-10-08 and the first of 2026-10-09 UTC (found by binary search on the
`/block` header times), the second a relayer's `MsgUpdateClient`. Mainnet never had a trading reward and runs no denom
rewards yet (v8.2.0): `TradingRewardDistributionEvent`,
`TradingRewardExpireEvent` and `DenomRewardScheduleFinishEvent` are synthetic.
Governance: 23745024 (`MsgSubmitProposal` of proposal 47
with its whole deposit), 23745061 (a validator owner's vote), 23745456 (a
delegator's vote) and 23821225 (`active_proposal`, passed); 20121960 carries
the legacy (v1beta1) submission of proposal 44. No recorded height has a
`MsgDeposit`, a weighted vote, a cancel or another resolution: those tests
encode the messages with the chain codec. No recorded height carries tokenfactory or tradebin
token activity (branding and halts exist only from chain v8.2.0, which
mainnet does not run yet): the token-event tests encode real messages with
the chain codec and give them the events the chain emits, and the cases are
checked live during the mainnet soak. The order book rows of the recorded
heights carry no message id (mainnet runs v8.1.1, and the testnet had no
order activity after its v8.2.0 upgrade at 3,626,000), so the attribution
by message id, the pending flag and fills written before their order are
tested on synthetic blocks: `internal/transform/dex_test.go` encodes the
messages with the chain codec, `e2e/dex_test.go` and `e2e/attribution_test.go`
write hand-built heights. The recorded heights carry the pre-v8.2.0 queue
order heuristic (`internal/transform/dexqueue_test.go`): 20300028 (v8.0.x,
one owner's buy rests and their sell of the same transaction fills an older
order and that buy), 24998316 (eleven orders of one owner in two
transactions, a third out of gas), 20234375 (a `MsgFillOrders` of five
fills) and 20230045 (v7.2). Recording a new height leaves `status.json` and `above_tip.json`
as committed (restore them with git) so the tests' tip stays put.

The transformer's golden files (`internal/transform/testdata`) are rewritten
with `go test ./internal/transform -golden`.

### Finding block-level heights

Block-level events have no transaction to search for, so their heights are
derived (one-off discovery, any route; the recording itself stays by height):

- **Epochs, burns, reward distributions**: mainnet REST
  `/bze/epochs/epochs` gives each epoch's `current_epoch_start_height`; that
  block carries the `EpochEndEvent`/`EpochStartEvent` pair. Staking rewards
  are distributed on the `day` epoch, the periodic burn runs on the `week`
  epoch whose ending number is a multiple of the burner's
  `periodic_burning_weeks` (4), trading rewards on the `hour` epoch. Earlier
  boundaries: subtract the epoch duration and binary search `/header` times,
  then scan a few `/block_results` around it.
- **Raffle results**: a `MsgJoinRaffle` (REST `/cosmos/tx/v1beta1/txs?query=
  message.action='/bze.burner.MsgJoinRaffle'`) at height H with N tickets
  draws at H+2 … H+N+1, one ticket per block; scan those `/block_results` for
  `RaffleWinnerEvent`/`RaffleLostEvent`. The raffle ends `duration` × 24 hour
  epochs after its `MsgStartRaffle`: binary search that time, then scan
  forward for `RaffleFinishedEvent`.
- **Unbonding and redelegation completions**: a `MsgUndelegate` or
  `MsgBeginRedelegate` older than the unbonding period (21 days) carries its
  `completion_time` in its `unbond`/`redelegate` event; the first block whose
  header time reaches it (binary search `/header`) ends with
  `complete_unbonding`/`complete_redelegation`, or the next one.

The script fetches `/block`, `/block_results` and `/commit` once per height,
plus `/status` and one above-tip answer. It never calls `/tx`, `/tx_search` or
`/block_search`.

### The fake gRPC server

`fakenode.NewGRPC` starts a real `grpc.Server` on a free local port serving
the query services of staking, slashing, bank, distribution, gov, mint, IBC
transfer and channel, tradebin, tokenfactory, rewards, burner, cointrunk and
txfeecollector. Each method answers from
`testdata/grpc/<service>/<Method>[.<key>].json`, the REST gateway's JSON
(the SDK's proto JSON), unmarshalled with the chain codec. The key is the
request's non-empty string, integer and enum fields in field order joined by
`.` (staking `Validator.<operator>`, `Delegation.<delegator>.<validator>`, gov
`Proposal.47`, `Proposals.PROPOSAL_STATUS_VOTING_PERIOD`), then the page key
of a request for a later page (`DenomOwners.<denom>.<next_key>`, "/" in the
base64 key escaped as `%2F`); without a
keyed file the keyless one answers, and without any file the method answers
`Unimplemented`. A recorded gateway error (`{"code": 5, "message": …}`)
answers that gRPC status. Tests count calls per method and per key, replace
an answer (`SetResponse`) and stop the server to play a node that is down.
So far the staking and slashing methods of the validators set, the account
page's bank, staking and distribution reads, the denoms set's bank,
tokenfactory, tradebin and IBC transfer methods, the holders set's
`DenomOwners`, the proposals set's gov and staking `Pool` methods, the
orders set's tradebin `UserMarketOrders` and the chain_state and params sets' pool, supply, mint, community pool and module
`Params` methods are recorded; each later story adds its
methods. Denoms in file names are path-escaped
(`DenomAuthority.factory%2Fbze1…%2Fuvdl.json`).

```
make grpc-fixtures VALIDATORS="bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m" ACCOUNTS="bze19fgph876c3rqxrn6xk5ch6wd73r3g05w690uls" REST=https://rest.getbze.com
```

`SETS` limits the run to some sets (`validators`, `accounts`, `denoms`, `gov`, `holders`, `orders`, `channels`, `chain`;
the first two by default), so adding an account does not re-record the
validators. `make grpc-fixtures SETS=denoms DENOMS="ubze factory/… ibc/…"`
records the denoms set: bank `TotalSupply` and `DenomsMetadata`, tradebin
`AllMarkets`, every factory denom's `DenomAuthority`, every IBC denom's
transfer `DenomTrace` (19 recorded 2026-10-09, three of them multi-hop),
and for each denom in
`DENOMS` its `DenomMetadataByQueryString` (a 404 for `ubze`) and `SupplyOf`.
tokenfactory `AllDenomBranding` and tradebin `HaltedDenoms` are recorded only
from a gateway that serves them (chain v8.2.0 and later); mainnet runs v8.1.1
(recorded 2026-10-09: 28 denoms with a supply, 28 with metadata, 12 markets),
so they are absent and answer Unimplemented like the node.
`tradebin/AllLiquidityPools.json` is hand-written, not recorded: the
`ulp_` pools of VDL/BZE and USDC.n/BZE (the third legacy LP, PHOTON/BZE, is
left out to exercise the fallback) and a hashed `ulp/<hash>` COREY/BZE pool
of chain v8.2.0+, whose denom only a test adds to the supply.
`make grpc-fixtures SETS=holders HOLDERS="factory/…/GGE" HOLDERS_PAGE_LIMIT=2`
records the holders set: `bank/DenomOwners.json`, the answer for a denom
nobody holds (the fallback for every other denom), and every page of each
listed denom (GGE: five owners on three pages of two, recorded 2026-10-09).
`make grpc-fixtures SETS=orders OWNERS="bze1…" ORDERS_HEIGHT=…` records the
orders set and the open orders route, every read pinned to that height:
each owner's `UserMarketOrders` (every market, one page of 1,000) and the
`MarketOrder` of each reference. `make fixtures ORDERS="bze1…@<height>"`
records the archive's `abci_query` answers of the same reads (`AllMarkets`,
`UserMarketOrders` in pages of 100 and every `MarketOrder`) at that height,
for the archive fallback; record both at one height. The market maker
`bze10kw8…` is recorded at 25,159,888 (2026-10-19, 29 orders in one market:
9 sells, 20 buys).
`make grpc-fixtures SETS=gov PROPOSALS="46 47"` records the proposals set:
gov `Proposals` (all, and the voting-period list), staking `Pool`, and each
listed proposal's `Proposal` and `TallyResult` (recorded 2026-10-09: 47
proposals, all passed, none voting; the e2e test turns proposal 47 back into
its voting period with `SetResponse`).
`make grpc-fixtures SETS=chain` records the chain_state and params sets:
staking `Pool` and `Params`, bank `SupplyOf` ubze, mint `Inflation`,
`AnnualProvisions` and `Params`, distribution `CommunityPool` and `Params`,
and the `Params` of slashing, gov (recorded from `/params/tallying`, whose
`params` is the whole set), tradebin, tokenfactory, rewards, burner,
cointrunk and txfeecollector (recorded 2026-10-10; the committed `Pool` and
`SupplyOf` ubze stay those of 2026-10-09, which the e2e numbers are computed
from).
`make grpc-fixtures SETS=channels` records the channels set: channel
`Channels` (mainnet's fourteen transfer channels, one page) and each one's
`ChannelClientState.transfer.<channel>.json` (recorded 2026-10-11), and
each client's `client/ClientStatus.<client>.json` (recorded 2026-10-12: six
of the fourteen clients expired).

records `staking/Validators.json`, `slashing/SigningInfos.json`,
`slashing/Params.json`, every validator's self-delegation and delegator
count, and, for each operator in `VALIDATORS`, its single `Validator` and
`SigningInfo` answers (ChainTools, the validator block 25000440 delegates
to). Recorded on 2026-10-09: 57 validators, 22 bonded. For each address in
`ACCOUNTS` it records `bank/AllBalances`, `staking/DelegatorDelegations`,
`staking/DelegatorUnbondingDelegations` and
`distribution/DelegationTotalRewards`: so far the owner of Thamar, who
signs at height 25000439 (recorded on 2026-10-09: three balances, one
delegation, no unbonding).

### The fake chain registry and aggregator

`fakeregistry.New` starts an `httptest` server standing in for the Cosmos
chain registry: its `APIURL` answers the contents API listings (the root
names every recorded chain, the directories `AddDirs` adds, `testnets` and
`_IBC`; `testnets/` lists nothing) and its `RawURL` serves
`testdata/<chain>/chain.json` and `assetlist.json`, 404 for anything else.
Tests count requests per path and turn it down (`SetDown`, 503). The files
are recorded from `raw.githubusercontent.com` by

```
make registry-fixtures CHAINS="beezee cosmoshub noble mirage"
```

(recorded 2026-10-09: BZE's own asset list maps `ubze` and its IBC denoms
to CoinGecko ids; `mirage` is a chain without explorers). The e2e harness
starts one with every sync environment, next to a fake aggregator answering
`/api/dex/tickers` as getbze.com did on 2026-10-09 and a fake CoinGecko
answering `/api/v3/simple/price` with the same day's prices, and points
`sync-state` and `serve` at them, so no test reaches GitHub, getbze.com or
CoinGecko. A `Config` built in code without `CoinGeckoHost` runs no prices
job and serves no `/api/prices`. A fake Keybase
lookup (`e2e/logos_test.go`) gives ChainTools' identity a picture and every
other identity none; `sync-state` and `serve` point `KEYBASE_URL` at it.
