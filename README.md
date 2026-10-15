# bze-scan

Block explorer for the BeeZee (BZE) blockchain. It is built for BZE's own modules
(tradebin DEX, burner, token factory, rewards, cointrunk, epochs) instead of the
generic Cosmos SDK view, so it can explain every transaction in plain words and
show activity that happens without a transaction (order fills, reward
distributions, raffles).

Status: the backend indexes the chain live and from the archive, keeps the
chain state in sync and serves the read API (`backend/README.md`). The UI has
its shell (header, search box, footer with the health pill), the API client
and the test harness (`ui/README.md`); its pages come next.

## Layout

- `backend/` — Go. One binary and, in production, one process: the read-only
  HTTP API, the live indexer, the state sync and the optional backfill run as
  goroutines with one log. Subcommands exist for migrations, reindexing and a
  standalone backfill.
- `ui/` — Next.js web app. Talks only to the backend API, raw JSON for the
  "More details" view included, and refreshes live views by polling every
  4 seconds.
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
a node. A status endpoint reports, once a minute, whether live indexing keeps
up with the node and an archive node and how far back the backfill has
reached; the UI shows it as a green or red light.

## License

MIT, see `LICENSE`.

## Development

- Go 1.26 for `backend/` (see `backend/README.md`).
- Node 24 for `ui/` (see `ui/README.md`).
- `docker/README.md` describes the local PostgreSQL setup, the two images
  (`ghcr.io/bze-alphateam/bze-scan-backend` and
  `ghcr.io/bze-alphateam/bze-scan-ui`) and the compose profile that runs them.

## Branches, releases and deploys

Two release branches, one network each. Work lands through pull requests to
`develop`; `develop` is merged into `main` when testnet looks right.

| Branch | Network | Image tag | Deployed to |
| --- | --- | --- | --- |
| `develop` | testnet (`bzetestnet-3`) | `testnet-<sha8>` | testnet-explorer.getbze.com |
| `main` | mainnet (`beezee-1`) | `<sha8>` and `latest` | explorer.getbze.com |

`.github/workflows/release.yml` runs on every push to either branch: the unit
and e2e tests of both apps, then both images, then the deploy. **One tag per
release:** both images are built on every push, whatever changed, so every
commit on a release branch has a complete backend + UI pair to deploy or roll
back to; the deploy target runs both from the same tag. The backend image is
the same for both networks; the UI's is not, because its `NEXT_PUBLIC_*`
values are inlined at build time from the deploy target's build env (see
`docker/README.md`).

The deploy step hands the tag to the BZE deployment repository's `deploy`
workflow (with the `DEPLOY_REPO_TOKEN` organisation secret) and waits for it:
the server migrates the database with the new backend image, starts the new
backend + UI next to the live pair, switches traffic once both are healthy
and stops the old pair. A failed migration or health check leaves the
previous release live and turns the run red. Migrations only add, so a
rollback is a deploy of an older tag.

Deploys from `main` run only while the repository variable `DEPLOY_MAINNET`
is `true`; until then a push to `main` publishes the images and stops.
