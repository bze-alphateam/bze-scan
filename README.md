# bze-scan

Block explorer for the BeeZee (BZE) blockchain. It is built for BZE's own modules
(tradebin DEX, burner, token factory, rewards, cointrunk, epochs) instead of the
generic Cosmos SDK view, so it can explain every transaction in plain words and
show activity that happens without a transaction (order fills, reward
distributions, raffles).

Status: repository skeleton, no code yet.

## Layout

- `backend/` — Go. One binary and, in production, one process: the read-only
  HTTP API, the live indexer, the state sync and the optional backfill run as
  goroutines with one log. Subcommands exist for migrations, reindexing and a
  standalone backfill.
- `ui/` — Next.js web app. Talks only to the backend API, raw JSON for the
  "More details" view included, and refreshes live views by polling every
  7 seconds.
- `docker/` — container images and the local-development compose setup
  (PostgreSQL with the CometBFT `psql` indexer schema plus the explorer schema,
  the backend and the UI). No node image: a local node is built from source.

## How it works

The explorer attaches to an existing `bzed` node whose CometBFT `psql` event
indexer already writes blocks, transactions and events into PostgreSQL. A
notification trigger on that indexer's `blocks` table wakes the explorer at the
commit of each block; the explorer then reads the block, its results and its
commit from the node by height, transforms them in Go and writes its own tables
in one transaction. The indexer's own tables are never read or deleted by the
explorer. History is backfilled from archive nodes through the same transformer
by a parallel pipeline, and a `reindex` command repairs any list or range of
heights. The UI shows a simple view by default and the full JSON on demand;
that JSON is proxied from archive nodes by the backend through a short
in-memory cache the live indexer fills as it goes, so the browser never calls
a node.

## License

MIT, see `LICENSE`.

## Development

- Go 1.26 for `backend/` (see `backend/README.md`).
- Node 24 for `ui/` (see `ui/README.md`).
- `docker/README.md` describes the local PostgreSQL setup.
