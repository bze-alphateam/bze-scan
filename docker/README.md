# docker

- Local development: a PostgreSQL 17 service initialised with the CometBFT
  `psql` indexer schema (vendored from the CometBFT version the chain runs,
  currently v0.38.26), so the backend's migrations and trigger tests run against
  the real layout.
- Production images: one Dockerfile per app (`backend`, `ui`). Deploy
  configuration (compose files, ports, virtual hosts) lives in the ops
  repository, not here.
