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
- `migrate` (see Migrations below).
- The live indexer inside `serve`, writing `explorer.blocks` (see Live
  indexer below). Transactions, messages and everything else follow.

## Configuration

Environment variables; a `.env` in the working directory is loaded when
present (variables already set in the environment win). `.env.dist` is the
documented template with the defaults:

| Variable | Default | Meaning |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | listen address of the HTTP API |
| `LOG_LEVEL` | `info` | logrus level |
| `LOG_FORMAT` | `text` | `text` or `json` |
| `DATABASE_URL` | none | PostgreSQL URL of the node's database; required by every command that touches it (`migrate`, and `serve` with the indexer). Never logged |
| `NODE_RPC_URL` | `http://127.0.0.1:26657` | CometBFT RPC of the local node, read by height only |
| `CHAIN_ID` | `beezee-1` | `serve` refuses to start the indexer when the node's `/status` reports another network |
| `INDEXER_ENABLED` | `true` | `false` runs the HTTP API only: the one way to run a second process against the same database |

Invalid values stop the process at startup with every problem listed.

## Commands

`make` (or `make help`) lists the targets:

| Target | What it does |
| --- | --- |
| `make build` | builds `build/bze-scan` |
| `make run` | runs `bze-scan serve` |
| `make migrate` | runs `bze-scan migrate up` against the compose database (or `DATABASE_URL` when set) |
| `make check` | everything CI runs, in CI's order |
| `make test` | unit tests, `go test ./... -race` (no network, no docker) |
| `make vet` | `go vet -tags=e2e ./...` |
| `make lint` | `golangci-lint run` (linters and formatters, acceptance tests included) |
| `make tidy-check` | fails when `go.mod`/`go.sum` are not tidy |
| `make vulncheck` | `govulncheck`; fails on a reachable vulnerability that has a fixed version, lists the ones without a fix |
| `make e2e` | starts PostgreSQL from `../docker/compose.yml`, runs the acceptance tests in `e2e/` (build tag `e2e`, `-race`), tears the database down; the exit code is the tests' |
| `make e2e-up` / `make e2e-down` | starts / removes that PostgreSQL by hand |
| `make fixtures HEIGHTS="..."` | records node fixtures for the fake node (see below) |
| `make clean` | removes `build/` |

The acceptance tests read `E2E_DATABASE_URL`, defaulting to the compose
database `postgres://bze:bze@127.0.0.1:15432/bze_index?sslmode=disable`.
CI runs three independent workflows on every pull request touching
`backend/` (and, for e2e, `docker/`), each its own check:

| Workflow | Runs |
| --- | --- |
| `backend-lint.yml` (Backend lint) | build, `make vet`, golangci-lint, `make tidy-check`, `make vulncheck` |
| `backend-unit.yml` (Backend unit tests) | `make test` |
| `backend-e2e.yml` (Backend e2e tests) | `make e2e` |

`make check` runs all of it locally.

## Layout

```
cmd/bze-scan/      main: runs app/cli
app/cli/           cobra root and subcommands (serve, migrate)
config/            environment parsing and validation
migrations/        SQL migrations (embedded, up and down), the Migrator,
                   post-migration steps, partition math
app/serve/         the serve process: its components in one errgroup
app/server/        echo wiring and the graceful HTTP runner
app/controller/    thin HTTP handlers
app/middleware/    request id, panic recovery, JSON error handler
internal/chain/    chain facts (bech32 prefix, module account addresses)
internal/node/     CometBFT RPC client, by-height routes only
internal/transform/
                   node answers of one height -> explorer rows (no I/O)
internal/writer/   transactional, idempotent writes (the live writer)
internal/indexer/live/
                   LISTEN/NOTIFY listener, height cursor, retries
internal/testutil/ acceptance-test helpers (database URL, Migrate)
internal/testutil/fakenode/
                   fake CometBFT RPC node for tests, fixtures in testdata/
e2e/               acceptance tests (build tag e2e)
scripts/           record-fixtures.sh
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
  (`blocks_p000024` holds [24,000,000, 25,000,000)).
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
  the transformer, then one transaction that inserts the `explorer.blocks`
  row (`ON CONFLICT DO NOTHING`; `block_time_ms` from the previous row when it
  is indexed), sets the live floor at the first write, and moves the cursor
  with `GREATEST`. When a height enters the last existing partition, the same
  transaction calls `explorer.ensure_partitions(height, height + 20000000)`.
- A height is tried three times (waits of 0.5 s and 2 s). Then, or at once
  when the node reports the height pruned, it is recorded in
  `explorer.index_failures` with source `live`, logged, and the cursor moves
  past it. A pass stops (and reconnects) only when the database or the
  node's `/status` cannot be reached, so a node restart never turns into
  recorded failures.
- On shutdown the height in flight gets up to 10 s to finish.
- Nothing is read from the sink's tables: the notification carries the
  height, the node supplies the data.

## Test fixtures and the fake node

`internal/testutil/fakenode` serves the URI form of the by-height RPC routes
(`/status`, `/block?height=N`, `/block_results?height=N`, `/commit?height=N`)
from recorded mainnet responses, byte for byte. A height without a fixture
answers the JSON-RPC error a real node returns above its tip (HTTP 500,
`testdata/above_tip.json`). Tests can override the `/status` height and read
per-route request counters.

Fixtures are committed and re-recorded only on purpose, when a test needs a
height the existing ones do not cover:

```
make fixtures HEIGHTS="24998316" ARCHIVE_RPC=https://rpc.getbze.com
```

Recorded today: 24998316 to 24998318 and 24998320. 24998319 is left out on
purpose: the fake node answers it with the above-tip error, which the live
indexer tests use as a failing height between good ones.

The script fetches `/block`, `/block_results` and `/commit` once per height,
plus `/status` and one above-tip answer. It never calls `/tx`, `/tx_search` or
`/block_search`.
