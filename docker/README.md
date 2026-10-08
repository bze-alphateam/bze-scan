# docker

- Local development: a compose file with PostgreSQL 17 initialised with the
  CometBFT `psql` indexer schema (vendored from the CometBFT version the chain
  runs, currently v0.38.26), the backend and the UI. The backfill pointed at a
  public archive node fills the database without any local node. The live path
  is exercised with a `bzed` built from source by the repository's Makefile
  target, state-synced to the network and configured to sink into this
  PostgreSQL. There is no `bzed` image.
- Production images: one Dockerfile per app (`backend`, `ui`). The node and
  PostgreSQL are not part of this repository's deployment: the explorer attaches
  to an existing node whose `psql` indexer already writes into an existing
  PostgreSQL. Deploy configuration (compose files, ports, virtual hosts) lives in
  the ops repository, not here.

## compose.yml

`compose.yml` (project name `bze-scan`) holds, for now, one service:
`postgres`, PostgreSQL 17 on host port 15432 with database `bze_index` and
user and password `bze`/`bze`. Its data directory is a tmpfs, so every start
is an empty database. At first start it runs
`postgres/initdb/01-cometbft-indexer-schema.sql`.

That file is the CometBFT v0.38.26 `psql` event sink schema, copied verbatim
from `state/indexer/sink/psql/schema.sql` of that module version, with a
header saying so. It is never edited: in production the node's sink writes
into exactly this schema, and the explorer's own objects come from its
migrations. When the chain moves to another CometBFT version, the file is
replaced with that version's schema.

The acceptance tests use it through the backend's Makefile:

```
cd backend
make e2e        # compose up -d --wait postgres, go test -tags=e2e ./e2e/, compose down -v
```

`make e2e-up` / `make e2e-down` start and remove the database by hand, for
example to run a single acceptance test with
`go test -tags=e2e -run TestName ./e2e/`. The backend and UI services join
this file later behind a profile, so `up postgres` keeps starting the
database alone.
