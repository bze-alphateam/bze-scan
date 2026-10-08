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

## Configuration

Environment variables; a `.env` in the working directory is loaded when
present (variables already set in the environment win). `.env.dist` is the
documented template with the defaults:

| Variable | Default | Meaning |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | listen address of the HTTP API |
| `LOG_LEVEL` | `info` | logrus level |
| `LOG_FORMAT` | `text` | `text` or `json` |
| `DATABASE_URL` | none | PostgreSQL URL of the node's database; required by every command that touches it (`migrate`). Never logged |

Invalid values stop the process at startup with every problem listed.

## Commands

`make` (or `make help`) lists the targets:

| Target | What it does |
| --- | --- |
| `make build` | builds `build/bze-scan` |
| `make run` | runs `bze-scan serve` |
| `make migrate` | runs `bze-scan migrate up` against the compose database (or `DATABASE_URL` when set) |
| `make test` | unit tests, `go test ./... -race` (no network, no docker) |
| `make vet` | `go vet ./...` |
| `make lint` | `golangci-lint run` |
| `make e2e` | starts PostgreSQL from `../docker/compose.yml`, runs the acceptance tests in `e2e/` (build tag `e2e`), tears the database down; the exit code is the tests' |
| `make e2e-up` / `make e2e-down` | starts / removes that PostgreSQL by hand |
| `make fixtures HEIGHTS="..."` | records node fixtures for the fake node (see below) |
| `make clean` | removes `build/` |

The acceptance tests read `E2E_DATABASE_URL`, defaulting to the compose
database `postgres://bze:bze@127.0.0.1:15432/bze_index?sslmode=disable`.
CI (`.github/workflows/backend.yml`) runs build, vet, lint, `make test` and
`make e2e` on every pull request touching `backend/` or `docker/`.

## Layout

```
cmd/bze-scan/      cobra root and subcommands (serve, migrate)
config/            environment parsing and validation
migrations/        SQL migrations (embedded, up and down), the Migrator,
                   post-migration steps, partition math
app/server/        echo wiring and the graceful HTTP runner
app/controller/    thin HTTP handlers
app/middleware/    request id, panic recovery, JSON error handler
internal/testutil/ acceptance-test helpers (database URL, Migrate)
internal/testutil/fakenode/
                   fake CometBFT RPC node for tests, fixtures in testdata/
e2e/               acceptance tests (build tag e2e)
scripts/           record-fixtures.sh
```

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

The script fetches `/block`, `/block_results` and `/commit` once per height,
plus `/status` and one above-tip answer. It never calls `/tx`, `/tx_search` or
`/block_search`.
