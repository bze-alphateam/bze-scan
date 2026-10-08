# bze-scan backend

Go module `github.com/bze-alphateam/bze-scan/backend`, Go 1.26 (the floor is set
by the `bze` chain module it imports to decode message types).

One binary, `bze-scan`, with subcommands:

- `serve` — the production process: the read-only HTTP API, the live indexer
  (woken by a PostgreSQL notification at each block the node's `psql` indexer
  commits, with a height cursor as the guarantee), the state sync (driven by
  what the indexer sees, with tickers as a safety net) and, when enabled by
  configuration, the backfill. One process, one log.
- `migrate` — applies the versioned migrations: the explorer schema, its
  partitions, the one notification trigger on the CometBFT `psql` indexer's
  `blocks` table, and the classification seed tables. The indexer's own tables
  are never altered otherwise.
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

## Commands

```
go build ./...
go vet ./...
go test ./... -race
```
