# bze-scan backend

Go module `github.com/bze-alphateam/bze-scan/backend`, Go 1.26 (the floor is set
by the `bze` chain module it imports to decode message types).

One binary, `bze-scan`, with subcommands:

- `serve` — read-only HTTP API over the explorer tables.
- `migrate` — applies the versioned migrations: additions to the CometBFT
  `psql` indexer schema (indexes, triggers), the explorer schema, the
  transformation functions and the classification seed tables.
- `enrich` — long-running: fills the fields the indexer does not carry (block
  hash, header time, proposer, signatures, memo, message bodies, gas, results of
  failed transactions) from the local node, by height.
- `backfill` — walks history backwards from archive nodes at a polite rate,
  normalises old event formats and writes them through the same path as live
  blocks. Checkpointed and resumable.
- `sync-state` — cron one-shot: validators, proposals, denominations, holders,
  labels, the Cosmos chain registry cache and token prices.
- `retention` — cron one-shot: prunes raw indexer rows, retries failed
  transformations, manages partitions, aggregates daily statistics.

## Commands

```
go build ./...
go vet ./...
go test ./... -race
```
