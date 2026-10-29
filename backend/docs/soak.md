# Mainnet soak

The gate before the first production deploy: the whole backend runs for 24 to
48 hours against a real mainnet node, built from source and state synced, on a
developer machine, and the numbers are recorded. Every command below runs from
`backend/` unless it says otherwise.

## Prerequisites

- Linux or macOS, Go 1.26 (the build fetches the Go line the chain's tag asks
  for, e.g. 1.25 for v8.1.1, through `GOTOOLCHAIN`), Docker with Compose v2.24
  or newer, `git`, `make`, `curl`, `jq`.
- 50 GB free disk: the node's data, the PostgreSQL volume and the Go caches.
- A machine that stays up, online and awake for 24 to 48 hours. Disable
  sleep.
- Inbound P2P is not required; the node dials its peers.

## What runs where

| Piece | Where | Data |
| --- | --- | --- |
| PostgreSQL 17 | `docker/compose.soak.yml` (project `bze-scan-soak`) on host port 15432 | named volume `bze-scan-soak_soak-postgres`, survives restarts |
| `bzed` | `.local/bin/bzed`, built from the tag mainnet runs | home `.local/bzed-home`; RPC `127.0.0.1:26657`, gRPC `127.0.0.1:9090`, P2P `:26656` |
| `bze-scan serve` | `build/bze-scan`, from this checkout | reads `.env`; API on `:8080`, pprof on `127.0.0.1:6060` |

The node's `psql` indexer writes into that PostgreSQL only; `make node-init`
refuses a `DATABASE_URL` that is not on localhost. Never point it at a shared
database.

The soak database and `make e2e` use the same host port: do not run the
acceptance tests while the soak runs (`make e2e` fails to start its own
PostgreSQL rather than touching the soak's).

## Order of operations

1. **Database.**

   ```bash
   make soak-db-up
   make migrate
   ```

   The first start initialises the CometBFT `psql` sink schema
   (`docker/postgres/initdb`); `migrate` adds the explorer schema and the
   notification trigger on the sink's `blocks` table.

2. **Node.**

   ```bash
   make node-build node-init
   ```

   `node-build` clones `github.com/bze-alphateam/bze` into `.local/bze` at the
   tag `https://rpc.getbze.com/abci_info` reports (override with
   `BZED_VERSION=v8.1.1`) and builds `bzed`; never a container image.
   `node-init` writes `.local/bzed-home`: the mainnet genesis from the tag,
   seeds and persistent peers from the chain registry, state sync against
   `rpc.getbze.com` and `rpc-1.getbze.com` with a trust height 2,000 blocks
   below the tip and its hash, `indexer = "psql"`,
   `discard_abci_responses = false`, `minimum-gas-prices` from the registry.
   The trust hash ages: if more than a week passes between `node-init` and
   `node-start`, run `make node-reset node-init` again.

3. **Start the node** in a terminal of its own:

   ```bash
   SOAK=1 make node-start
   ```

   `node-start` refuses without `SOAK=1`. State sync finds a snapshot,
   restores it, then block-syncs the rest; wait for

   ```bash
   curl -s 127.0.0.1:26657/status | jq '.result.sync_info | {latest_block_height, catching_up}'
   ```

   to show `catching_up: false`. If no snapshot is offered within about ten
   minutes, stop the node, `make node-reset node-init` and start again (the
   peers rotate their snapshots).

4. **Explorer.** Create `.env` from `.env.dist` and change:

   ```bash
   BACKFILL_ENABLED=true
   BACKFILL_FLOOR=<the date seven days back, YYYY-MM-DD>
   DEBUG_ADDR=127.0.0.1:6060
   ```

   then, in another terminal:

   ```bash
   make build && build/bze-scan serve 2>&1 | tee -a soak.log
   ```

   The live indexer starts at the node's head; the backfill walks down from
   there to the floor through the archive.

5. **UI (optional)**, once the UI image exists in `docker/compose.yml`'s
   `full` profile: start it against `http://localhost:8080`.

6. **Live transaction checklist.** Once the explorer is healthy, submit the
   checklist's transactions on mainnet (Claude hands the list over at the
   start of the soak; it lives in the soak ticket) and check each on the API
   and, if it is up, the UI.

## What to watch

Check every few hours, and note the time of every check:

| What | How | Expected |
| --- | --- | --- |
| Status | `curl -s localhost:8080/api/v1/status \| jq` | `live_fill.healthy` true throughout except around a restart; `back_fill.status` `in_progress`, then `finished` |
| Lag | `db_height` against `node_height` and `archive_height` in the status | within 5 blocks; note the maximum |
| Failures | `SELECT source, count(*) FROM explorer.index_failures WHERE resolved_at IS NULL GROUP BY 1;` | 0, or a fixed set that never grows; every row explained |
| Backfill | `SELECT status, lowest_dispatched, resume_height, floor_height, blocks_done, updated_at FROM explorer.backfill_checkpoints WHERE job = 'main';` | `blocks_done` growing at about `BACKFILL_RATE_LIMIT / 3` heights per second (20 requests/s: about 6.7) until `done` |
| Memory | `ps -o rss= -p "$(pgrep -f 'bze-scan serve')"` (KiB) | flat once the backfill is done; note min and max |
| Goroutines | `curl -s 127.0.0.1:6060/debug/pprof/goroutine?debug=1 \| head -1` | flat; a steady climb is a leak (save `curl -s -o goroutines.txt '127.0.0.1:6060/debug/pprof/goroutine?debug=2'`) |
| Heap | `go tool pprof -top http://127.0.0.1:6060/debug/pprof/heap` | no type growing between checks |
| Database size | `SELECT pg_size_pretty(pg_database_size('bze_index'));` and `SELECT schemaname, pg_size_pretty(sum(pg_total_relation_size(schemaname \|\| '.' \|\| tablename))) FROM pg_tables WHERE schemaname IN ('public', 'explorer') GROUP BY 1;` | the `explorer` schema grows about 18 MB per day of chain indexed (the sizing estimate); up to twice that passes |
| Archive load | the backfill rate above; `grep -c 'backfill: fetch failed' soak.log` for the retried fetches | never above `BACKFILL_RATE_LIMIT` requests per second |
| Log noise | `grep -E 'level=(warning\|error)' soak.log \| sort \| uniq -c \| sort -rn \| head` | no repeating warning without a cause |

Run the SQL with

```bash
docker compose -f ../docker/compose.yml -f ../docker/compose.soak.yml exec postgres psql -U bze -d bze_index
```

## Stop and resume

- Stop the explorer with Ctrl-C (SIGTERM): the HTTP server drains, the live
  indexer finishes the height in flight, the backfill flushes what it holds
  and saves its checkpoint. Then stop the node the same way.
- Restart in the reverse order: `SOAK=1 make node-start`, then
  `build/bze-scan serve`. Check that the backfill resumes at its
  `resume_height` (the log says where it starts) and that the live
  indexer catches up the blocks the node produced meanwhile without new
  `index_failures`. Record each restart, planned or not.
- `make soak-db-down` stops the database and keeps its volume.

## What to record

Post one comment on the soak ticket with:

| Item | Value |
| --- | --- |
| Start and end (UTC) and duration | |
| bzed version | |
| Start and end heights (live floor, last indexed) | |
| Backfill floor reached (height, date) and blocks per second | |
| Maximum lag observed (blocks) | |
| `index_failures` (count by source, explained) | |
| Database size at start and end, `explorer` schema growth per day indexed | |
| RSS minimum and maximum, goroutines at start and end | |
| Restarts (time, reason) | |
| Live transaction checklist (each item checked or failed) | |
| Anomalies, each with its follow-up ticket | |

Every anomaly becomes its own ticket before the production deploy starts.

## Clean up

```bash
make node-reset
docker compose -f ../docker/compose.yml -f ../docker/compose.soak.yml down -v
rm -rf .local
```
