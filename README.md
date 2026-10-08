# bze-scan

Block explorer for the BeeZee (BZE) blockchain. It is built for BZE's own modules
(tradebin DEX, burner, token factory, rewards, cointrunk, epochs) instead of the
generic Cosmos SDK view, so it can explain every transaction in plain words and
show activity that happens without a transaction (order fills, reward
distributions, raffles).

Status: repository skeleton, no code yet.

## Layout

- `backend/` — Go. One binary that serves the read-only HTTP API from the
  explorer's PostgreSQL tables and runs the processes that fill them
  (migrations, enricher, backfill, state sync, retention).
- `ui/` — Next.js web app. Talks to the backend API, and lazily loads raw block
  and transaction JSON from BZE archive nodes for the "More details" view.
- `docker/` — container images and the local-development compose setup
  (PostgreSQL with the CometBFT `psql` indexer schema plus the explorer schema).

## How it works

A dedicated pruned `bzed` node writes blocks, transactions and events into
PostgreSQL through CometBFT's built-in `psql` event indexer. Database triggers
transform those raw rows into the explorer's own tables at commit time. The Go
backend serves the API from those tables, enriches blocks with the few fields
the indexer does not carry, backfills history from archive nodes, and prunes raw
rows after a retention window. The UI shows a simple view by default and the
full JSON on demand.

## License

MIT, see `LICENSE`.

## Development

- Go 1.26 for `backend/` (see `backend/README.md`).
- Node 24 for `ui/` (see `ui/README.md`).
- `docker/README.md` describes the local PostgreSQL setup.
