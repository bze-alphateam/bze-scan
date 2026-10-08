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
