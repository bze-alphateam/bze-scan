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
  holders, labels, the Cosmos chain registry cache and token prices, for
  operations.
- `version` — prints the commit the binary was built from (`dev` for a plain
  `go build`; the image sets it with `-ldflags "-X main.version=<sha>"`).

Every node call is by height (`/block`, `/block_results`, `/commit`, `/status`
and gRPC state queries); the search routes are never used, on any node.

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
  validators and denoms sets, `GET /api/v1/validators` and the token routes
  (see State sync below).

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
| `AGGREGATOR_URL` | empty | base URL of the BZE aggregator (production `https://getbze.com`); its `/api/prices` feeds the prices job every minute; empty turns the job off |
| `PRICE_CHANGE_MARKET` | mainnet's BZE/USDC.n pool, `ibc/6490…8AF4_ubze` | the aggregator market whose ticker gives BZE's 24-hour change; change it when the USDC pool moves (the USDC.n → USDC.inj migration); empty leaves the change null |
| `CHAIN_REGISTRY_API_URL` | `https://api.github.com/repos/cosmos/chain-registry/contents` | GitHub contents API of the Cosmos chain registry, listed by the chain_registry job |
| `CHAIN_REGISTRY_RAW_URL` | `https://raw.githubusercontent.com/cosmos/chain-registry/master` | base of the registry's raw `chain.json` / `assetlist.json` files |

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
| `bze-scan sync-state` | one full resync of every state-sync set (see State sync below); exit 0 when every set succeeded, 1 when any failed |
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
| `make clean` | removes `build/` |

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
                   reindex) and their exit codes
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
internal/node/     CometBFT RPC client, by-height routes only
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
internal/labels/   the module and known account labels `migrate` seeds
internal/statesync/
                   the state sync: dirty sets, coalescing queue, workers,
                   safety-net timers, sync_jobs
internal/statesync/validators/
                   the validators set (explorer.validators and the
                   validator_owner labels)
internal/statesync/denoms/
                   the denoms set (explorer.denoms, IBC denom resolution)
internal/statesync/registry/
                   the chain_registry set (explorer.chains, registry_assets)
internal/statesync/holders/
                   the holders set (token_holders, denoms.holders_count)
internal/statesync/prices/
                   the prices set (denoms.price_usd)
internal/chainregistry/
                   the Cosmos chain registry reader (GitHub contents API +
                   raw files)
internal/aggregator/
                   the BZE aggregator's /api/prices reader
internal/rawcache/ in-memory LRU of raw node responses, archive fetch on a
                   miss
internal/status/   the status checker and its snapshot
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
routes, which ask an archive node on a cache miss. Conventions, which
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
  key.
- **Errors** answer `{"error": {"code": "...", "message": "..."}}` with
  `bad_request` (400), `not_found` (404), `upstream_error` (502/504, for the
  routes that call a node) or `internal` (500, details only in the log).
- **Caching**: lists, search, validators, accounts and every error are
  `Cache-Control: no-store`; a block by height and a found transaction are
  `public, max-age=31536000, immutable` (a committed height never changes),
  except a block whose proposer is not named yet (no synced validator has its
  consensus address), which stays `no-store` until it is.
- **CORS** only for the origins in `CORS_ALLOWED_ORIGINS`.
- **Request log**: one info line per request with method, path, status,
  duration and request id.

| Route | Answers |
| --- | --- |
| `GET /api/v1/blocks?cursor&limit` | blocks, height descending: height, time, hash, tx_count, tx_failed_count, proposer_cons_address, block_time_ms, size_bytes, `proposer` (`{operator_address, moniker}` of the validator with that consensus address, null while none is synced) |
| `GET /api/v1/blocks/{height}` | every column of the block plus `transactions` (height, tx_index, hash, success, msg_types, fee, first signer), `transfers` (the block's own moves, as on the transaction page, `msg_index` null) and `events` (its first 100 stored block events: seq, type, attrs) with `events_next_cursor` (null when they are all there); 400 unless a positive integer, 404 when not indexed |
| `GET /api/v1/blocks/{height}/events?cursor&limit` | the block's stored events in order (seq, type, attrs); immutable once the block is indexed; 404 when not indexed |
| `GET /api/v1/txs?cursor&limit&status=success\|failed` | transactions, height and index descending: height, tx_index, hash, time, success, msg_count, msg_types, fee, first signer |
| `GET /api/v1/txs/{hash}` | every column of the transaction plus `messages` (msg_index, type_url, sender, module, body, events) and `transfers` (seq, msg_index (null for the fee), kind `transfer`/`mint`/`burn`, sender, recipient, denom, amount in base units, `symbol`/`exponent` when the denom is known, `sender_label`/`recipient_label` `{name, kind}` when the address is labelled); the hash is 64 hex characters in any case (else 400); 404 when not indexed, which the UI shows as pending |
| `GET /api/v1/validators?status=bonded\|unbonding\|unbonded\|all&cursor&limit` | validators (all statuses by default): the bonded ones by `rank`, then the others by tokens; rank, moniker, operator_address, tokens, voting_power_pct, commission_rate, uptime, jailed, status. `voting_power_pct` (share of the bonded tokens) and `uptime` (`1 − missed / window` of the slashing signing info) are percentages with five decimals; rank and voting power are null outside the active set; another status is a 400 |
| `GET /api/v1/validators/{operator}` | every column of the validator (the list item plus account_address, consensus_address and key, description fields, tombstoned, jailed_until, commission limits and update time, min_self_delegation, self_delegation, delegator_count, missed_blocks, signed_blocks_window, first_seen_height/time, updated_at), `recent_blocks` (the last 10 it proposed), `events` (its last 20 `validator_events`, newest first, with `tx_hash` null for block-level and sync-found ones) and `votes` (the owner account's last 20 governance votes); 400 unless a `bzevaloper1…` address, 404 when not synced |
| `GET /api/v1/validators/{operator}/blocks?cursor&limit` | the blocks it proposed, height descending, as the block list; empty without a consensus address |
| `GET /api/v1/accounts/{address}` | `address`, `label` (`{name, kind}` or null), `first_seen` (`{height, time}` of its first signed transaction, null when the explorer never saw it), `last_seen_height`, `tx_count`, `activity_count`, then the live state read from the node: `balances` (`{denom, amount, symbol, exponent}`, symbol and exponent from `denoms`, null when unknown), `delegations` and `unbonding` (`{validator, moniker, amount}`, unbonding with `completion_time`, one item per entry), `rewards` (`{validator, coins}`), `total_staked` (base units of the bond denom) and `total_rewards` (coins), rewards truncated to whole base units, and `live: {available}`. 400 unless a `bze1…` address; an address the explorer never saw is a 200 with null `first_seen`. When the node cannot be read the answer is still a 200 with the indexed part, empty live lists, null `total_staked` and `live.available` false |
| `GET /api/v1/tokens?kind=native\|factory\|ibc\|lp\|unknown&cursor&limit` | every denom by kind (native, factory, ibc, lp, unknown) then symbol: denom, symbol, name, kind, exponent, supply (base units), holders_count (owners of at least one display unit, null until the holders job), price_usd (the aggregator's USD price, null until the prices job and for a denom it does not price), price_change_24h_pct (`ubze` only: BZE's 24-hour change in percent, four decimals, from the BZE/USDC pool's ticker; null for every other denom), halted, logo_url, `origin_chain` (an IBC denom's `{chain_id, name, logo_url}`, name and logo from the chain registry and null for a chain it does not know; null for other denoms and until the denom's channel is known); another kind is a 400 |
| `GET /api/v1/tokens/{denom}` | every column of the denom (the list item plus description, the IBC origin fields, creator and admin with their labels (admin null once renounced), created_height/tx_hash/time from its `created` event, website, markets, raw bank `metadata`, updated_at), `events` (its last 20 token events, newest first) and `events_next_cursor`; the denom is URL-encoded (`factory%2Fbze1…%2Fuhoney`) since it contains `/`; 404 when unknown |
| `GET /api/v1/tokens/{denom}/events?cursor&limit` | the denom's token events, newest first: height, tx_index, seq, tx_hash (null for a block-level halt), kind, actor (and `actor_label`), amount, details, time |
| `GET /api/v1/tokens/{denom}/transfers?cursor&limit` | every indexed move of the denom, newest first: height, tx_index, tx_hash, time and the transfer as on the transaction page |
| `GET /api/v1/tokens/{denom}/holders?cursor&limit` | the last holders snapshot, largest balance first: rank, address, `label`, balance (base units) and `share_pct` (of the supply, five decimals, null without a supply); every non-zero balance, refreshed hourly |
| `GET /api/v1/token?denom=`, `/api/v1/token/events?denom=`, `/api/v1/token/transfers?denom=`, `/api/v1/token/holders?denom=` | the same four with the denom as a query value, the chain's own convention for denoms with `/` |
| `GET /api/v1/proposals?status=deposit_period\|voting_period\|passed\|rejected\|failed\|canceled&cursor&limit` | every proposal, id descending: id, title, kind (`software_upgrade`, `community_pool_spend`, `parameter_change`, `cointrunk_publisher`, `ibc_client_update`, `text`, `other`), status, expedited, submit/deposit-end/voting-start/voting-end times, `tally` (`{yes, no, abstain, no_with_veto}` in base units, null until synced) and `turnout_pct` (the tally's sum over the bonded tokens it was measured against, five decimals, null without them); another status is a 400 |
| `GET /api/v1/proposals/{id}` | the list item plus summary, metadata, proposer (and `proposer_label`), message_types, `messages` (proto JSON; a legacy content inside its `MsgExecLegacyContent`), total_deposit, tally_bonded_tokens, tally_updated_at, submit_height and submit_tx_hash (null for a proposal submitted below the indexed range), resolved_height, `validators_voted` (`{voted, total}`: bonded validators whose owner voted, of all bonded validators) and updated_at; 404 when unknown |
| `GET /api/v1/proposals/{id}/votes?option=yes\|no\|abstain\|no_with_veto\|weighted&cursor&limit` | every vote the explorer indexed (the chain deletes them once tallied), newest first, one per voter (the latest): voter (and `voter_label`), option (null for a split vote; `weighted` lists those), options (the weights as SDK v0.50 encodes them), height, tx_index, tx_hash, time, and for a validator owner `validator` (moniker), `validator_operator` and `voting_power_pct` (its current share), null for other voters |
| `GET /api/v1/proposals/{id}/deposits?cursor&limit` | every deposit, the initial one included, newest first: depositor (and `depositor_label`), amount (coins), height, tx_index, tx_hash, time |
| `GET /api/v1/search?q=` | `{"results": [{"type", "id", "label"}]}`: digits find an indexed block, 64 hex characters an indexed transaction, a `bze1…` address an account (always returned, with `indexed` true or false), a `bzevaloper1…` address a known validator; any other text of two characters or more the validators whose moniker contains it and the labelled accounts whose name contains it (any case, five of each, validators first, accounts labelled by name); no match is an empty list; an empty `q` is a 400 |
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
indexed it or not. Successes are `immutable`; when no archive node serves the
height (unreachable, an error, above its tip) the answer is 502
`upstream_error` and nothing is cached. The cache keeps one LRU per route of
at most `RAW_CACHE_MAX_ENTRIES` entries, each living `RAW_CACHE_TTL` from the
moment it was stored; an entry is never refreshed (a height never changes).
The live indexer puts the three bodies of every height it writes, so recent
heights never reach the archive. A miss fetches from `ARCHIVE_RPC_URL`, then
once from `ARCHIVE_RPC_RETRY_URL` (or the same URL again); concurrent misses
for one height share one fetch, which outlives a request that gives up.

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
  operational tables, and the functions (`ensure_partitions`,
  `reclassify_unknown`).
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
  `validator_owner` rows.
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
  past it. A pass stops (and reconnects) only when the database or the
  node's `/status` cannot be reached, so a node restart never turns into
  recorded failures.
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
  every worker, retries three times (0.5 s, 1 s, 2 s) against
  `ARCHIVE_RPC_RETRY_URL` (or the primary again), runs the archive adapter
  (see "Old event formats") and the transformer, and sends one result into
  the writer's channel (capacity X). Its slot frees once the result is
  queued, so memory holds at most X in flight, X queued and M held.
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
- **Checkpoint.** `explorer.backfill_checkpoints` row `main` (ceiling, floor,
  `lowest_dispatched`, `blocks_done`, status, last error), saved every 5 s
  and after every flush. Dispatch is monotonic, so on restart the dispatcher
  starts X + M heights above `lowest_dispatched` and skips the heights
  already in `explorer.blocks` (one query per 1,000 heights). Status:
  `running`, `done` at the floor, `error` after a fatal error (resumed at the
  next start), `paused` while `BACKFILL_ENABLED=false`. A done job is not
  rerun unless the floor moves lower.

### Old event formats

Heights before v8.0.0 (mainnet height 20,237,800) were produced by Cosmos SDK
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
stored. A type added to the classification later needs a reindex of the
range. The wording a page shows ("transaction fee", "paid to the seller") is
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
  later (the backfill walks down) never does.
- `cancel_proposal` sets status `canceled` (the chain deletes the proposal).

From the block's finalize events, `active_proposal` sets `passed`,
`rejected` or `failed` from `proposal_result` with `resolved_height`
(`expedited_proposal_rejected` puts an expedited proposal back into its
voting period as a regular one), and `inactive_proposal` sets `rejected`
(dropped for lack of deposit) or `failed`. Both are also `block_events`
rows. A resolution never goes back to an earlier one, and an open status
never undoes a resolution. Every proposal these events name is dirty for
the proposals set.

### Accounts

Every signer of every transaction gets an `explorer.accounts` row:
`first_seen_height`/`first_seen_time` move back with `LEAST`,
`last_seen_height` forward with `GREATEST`, and `tx_count` counts the
transactions the write actually inserted (the keys the `transactions`
insert returned), so writing a height again, or a reindex, never counts
twice. `activity_count` stays 0 until the activity feed. Both writers merge
the deltas of a write per address and apply them in address order
(`mergeDeltas` in `internal/writer`, shared with the later per-token
totals), so concurrent writes lock the rows in the same order; a write that
still hits a deadlock (SQLSTATE 40P01) is retried once.

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
  it, with the retries on `ARCHIVE_RPC_RETRY_URL`. A height the local node
  pruned during the run goes to the archive too; when the local node does
  not answer `/status`, every height does. `BACKFILL_RATE_LIMIT` paces all
  requests, local ones included. Old heights pass through the archive
  adapter as in the backfill.
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
   dirty set (validators, proposals, denoms, channels, params; validators
   and denoms are marked so far) and the live indexer publishes it after the
   write. The backfill and the reindex ignore it: history cannot change
   current state. Keys go to an in-memory queue served by two workers; a
   key already queued is not queued twice, and a queued full resync absorbs
   the keys of its set;
3. resyncs a set in full when none ran for its interval (validators,
   proposals, prices: one minute; denoms, holders: one hour; chain
   registry: one day), the safety net for what no event announces. A full resync, from
   a block or the timer, restarts the wait.

Every run is recorded in `explorer.sync_jobs` (`last_run_at`,
`last_success_at`, `last_error`, `cursor`). A node that is down fails the
run: it is logged and recorded (the prices set logs a failing aggregator once
per change of state, not every minute), the rows stay as they were, and the next
trigger retries. Nothing here is on the user's critical path. `bze-scan
sync-state` runs the full resync of every set once and exits 1 when any
failed.

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
and `HaltedDenoms`, and per factory denom tokenfactory `DenomAuthority`.
Supply and metadata always come from the node, never from events. `kind` is
the denom's shape: `native` for `ubze`, `factory` for `factory/…`, `ibc` for
`ibc/…`, `lp` for tradebin pool shares (`ulp/<hash>` since chain v8.2.0,
`ulp_<base>_<quote>` before), else `unknown`. `symbol`, `name`,
`description` and `logo_url` (the metadata `uri`) come from the bank
metadata and `exponent` is its display unit's (0 when the display unit is not
listed, as in the placeholder metadata ibc-go writes for every IBC voucher);
`ubze` has
no bank metadata and takes the chain's constants (BZE, exponent 6). A factory
denom's `creator` is the address in it and `admin` the tokenfactory admin
(NULL once renounced). `markets` are the tradebin market ids (`base/quote`)
the denom trades in; `halted` is the tradebin halt (a node before v8.2.0
answers `HaltedDenoms` with Unimplemented: nothing is halted). First sight
(`created_*`) is the denom's earliest `created` token event, filled at the
next resync once the event is indexed. A denom the bank no longer lists keeps
its row with a zero supply. The holders and prices columns are left to their
own jobs.

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
denom stay. A chain or an asset the registry cache misses is published to
the chain_registry set (by chain id, or `name:<registry name>` for a trace's
chain).

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

**Prices.** With `AGGREGATOR_URL` set, every minute `GET /api/prices` of the
BZE aggregator gives USD prices by CoinGecko id (`bzedge`, `cosmos`, …). A
denom takes the price of its registry asset's CoinGecko id: BZE's own asset
list first (`ubze` is `bzedge`, and it lists the IBC denoms BZE holds), else
the origin chain's asset. A denom no longer priced is cleared; a failed fetch
keeps every price. The explorer computes no DEX price. `ubze`'s
`price_change_24h_pct` comes from the aggregator's `/api/dex/tickers`, read at
most every 5 minutes: the ticker of `PRICE_CHANGE_MARKET` (the BZE/USDC.n
liquidity pool, which prices USDC.n in ubze), as last over open when `ubze`
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
pool's bonded tokens (`tally_bonded_tokens`, for turnout), `tally_updated_at`
the sync's time; once resolved it is the proposal's final tally, as of its
voting end, and the last bonded tokens are kept. An answer read before a
resolution was indexed never overwrites the resolved status or tally. A
proposal the node no longer has (dropped, canceled) keeps the row its events
wrote. A proposal the sync writes before its resolution is indexed takes
`resolved_height` from the stored `active_proposal`/`inactive_proposal`
block event.

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

## Test fixtures and the fake node

`internal/testutil/fakenode` serves the URI form of the by-height RPC routes
(`/status`, `/block?height=N`, `/block_results?height=N`, `/commit?height=N`)
from recorded mainnet responses, byte for byte. A height without a fixture
answers the JSON-RPC error a real node returns above its tip (HTTP 500,
`testdata/above_tip.json`). Tests can override the `/status` height, make the
node pruned below a height (`SetEarliestHeight`: `/status` reports it and
lower heights answer the node's "not available" error), and read request
counters per route and per height.

Fixtures are committed and re-recorded only on purpose, when a test needs a
height the existing ones do not cover:

```
make fixtures HEIGHTS="24998316 24998321..24998330" ARCHIVE_RPC=https://rpc.getbze.com
```

Recorded today: 24998316 to 24998330 except 24998319, which is left out on
purpose: the fake node answers it with the above-tip error, which the live
indexer and backfill tests use as a failing height between good ones. The
backfill tests walk that whole range (24998316, 24998321 and 24998326 carry
tradebin transactions). Heights with
transactions: 24999004 (multi-message `MsgCancelOrder` transactions and an
out-of-gas `MsgCreateOrder`), 24999134 (multi-message `MsgCreateOrder`),
24999205 (IBC `MsgTransfer`), 24999209 (relayer `MsgUpdateClient` with
`MsgRecvPacket` / `MsgAcknowledgement`), 25000439 (`MsgWithdrawDelegatorReward`
+ `MsgWithdrawValidatorCommission`), 25000440 (authz `MsgExec`) and 25000894
(`MsgSend`). Validator events: 22933748 (`MsgEditValidator` raising a
commission), 24113494 (`MsgCreateValidator`), 24129272 (`MsgUnjail`),
24151894 (`MsgEditValidator` of a description) and 24160001 (a downtime
`slash`, its `liveness` event and the validator update removing it from the
active set). Transfers and block events come from the same heights: the fills
of 24999134 (settlements at block level, `OrderExecutedEvent`), the failed
transaction of 24999004 (its fee row only), the slash of 24160001 (a burn) and
the empty 24998317. Governance: 23745024 (`MsgSubmitProposal` of proposal 47
with its whole deposit), 23745061 (a validator owner's vote), 23745456 (a
delegator's vote) and 23821225 (`active_proposal`, passed); 20121960 carries
the legacy (v1beta1) submission of proposal 44. No recorded height has a
`MsgDeposit`, a weighted vote, a cancel or another resolution: those tests
encode the messages with the chain codec. No recorded height carries tokenfactory or tradebin
token activity (branding and halts exist only from chain v8.2.0, which
mainnet does not run yet): the token-event tests encode real messages with
the chain codec and give them the events the chain emits, and the cases are
checked live during the mainnet soak. Recording a new height leaves `status.json` and `above_tip.json`
as committed (restore them with git) so the tests' tip stays put.

The transformer's golden files (`internal/transform/testdata`) are rewritten
with `go test ./internal/transform -golden`.

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
`DenomOwners` and the proposals set's gov and staking `Pool` methods are
recorded; each later story adds its
methods. Denoms in file names are path-escaped
(`DenomAuthority.factory%2Fbze1…%2Fuvdl.json`).

```
make grpc-fixtures VALIDATORS="bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m" ACCOUNTS="bze19fgph876c3rqxrn6xk5ch6wd73r3g05w690uls" REST=https://rest.getbze.com
```

`SETS` limits the run to some sets (`validators`, `accounts`, `denoms`, `gov`;
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
`make grpc-fixtures SETS=holders HOLDERS="factory/…/GGE" HOLDERS_PAGE_LIMIT=2`
records the holders set: `bank/DenomOwners.json`, the answer for a denom
nobody holds (the fallback for every other denom), and every page of each
listed denom (GGE: five owners on three pages of two, recorded 2026-10-09).
`make grpc-fixtures SETS=gov PROPOSALS="46 47"` records the proposals set:
gov `Proposals` (all, and the voting-period list), staking `Pool`, and each
listed proposal's `Proposal` and `TallyResult` (recorded 2026-10-09: 47
proposals, all passed, none voting; the e2e test turns proposal 47 back into
its voting period with `SetResponse`).

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
`/api/prices` as getbze.com did on 2026-10-09, and points `sync-state` and
`serve` at both, so no test reaches GitHub or getbze.com.
