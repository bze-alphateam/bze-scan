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

`compose.yml` (project name `bze-scan`) holds three services. The first is
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
`go test -tags=e2e -run TestName ./e2e/`.

The second is `backend`, behind the `full` profile, so `make e2e` and a plain
`up` start the database alone. It builds the image below from this checkout,
reads `backend/.env` (copy `backend/.env.dist`; optional) and overrides
`DATABASE_URL` with the compose `postgres` and `HTTP_ADDR` with `:8080`
(published on host port 8080). A node running on the host is reachable as
`host.docker.internal`, not `127.0.0.1`. It starts once PostgreSQL is
healthy, is healthchecked with `wget` against `/health`, and gets a 15 s stop
grace period (`serve` drains HTTP for up to 10 s on SIGTERM).

```
docker compose -f docker/compose.yml --profile full up -d --build
docker compose -f docker/compose.yml --profile full run --rm backend migrate up
docker compose -f docker/compose.yml --profile full run --rm backend sync-state
docker compose -f docker/compose.yml --profile full down -v
```

Every other subcommand (`backfill`, `reindex ...`, `version`) runs as a
one-shot the same way: the image's entrypoint is the `bze-scan` binary and
`serve` is only its default command.

The third is `ui`, also behind `full`. It builds the UI image with `ui/.env`
(copy `ui/.env.dist`) as its build env, so the browser calls the backend on
`http://localhost:8080/api`, while the Next.js server reaches it on the compose
network (`API_URL=http://backend:8080/api`). It is published on host port
3000, starts once the backend is healthy, is healthchecked with `wget`
against `/` and gets a 10 s stop grace period. `up --profile full` then serves
the explorer on http://localhost:3000.

## compose.soak.yml

An override for the mainnet soak (`backend/docs/soak.md`): the same
`postgres` service on a named volume instead of tmpfs, so the data survives
restarts, under the project name `bze-scan-soak`, so `make e2e` never removes
it. It uses the same host port 15432: run one or the other.

```
docker compose -f docker/compose.yml -f docker/compose.soak.yml up -d --wait postgres
docker compose -f docker/compose.yml -f docker/compose.soak.yml down        # keeps the volume
```

## Images

`backend.Dockerfile` builds `ghcr.io/bze-alphateam/bze-scan-backend`. The
build context is the repository root, trimmed by the root `.dockerignore` to
the backend sources (no `.env`, no UI, no test data):

```
docker build -f docker/backend.Dockerfile --build-arg GIT_SHA=$(git rev-parse --short=8 HEAD) -t bze-scan-backend .
```

- Build stage: `golang:1.26-alpine` with `GOTOOLCHAIN=auto`, so the exact
  toolchain of `backend/go.mod` is used; `CGO_ENABLED=0`, `-trimpath`,
  `-ldflags "-s -w -X main.version=$GIT_SHA"`.
- Runtime stage: Alpine, the static binary at `/usr/local/bin/bze-scan` as
  entrypoint, `serve` as default command, user `bze` (uid 10001), port 8080
  exposed. Alpine rather than distroless so a healthcheck has `wget`.
- OCI labels `org.opencontainers.image.source` and `.revision` (the sha).
- No configuration, no secrets and no baked-in healthcheck: everything comes
  from the environment at run time, and whoever runs the image sets the
  healthcheck against `/health` and a stop grace period of 15 s.

CI (`.github/workflows/backend-image.yml`) builds the image on every pull
request and runs `smoke-test.sh` on it: `version` must print the commit,
`serve` must answer `GET /health` with 200 within 10 s and stop with exit
code 0 within 15 s of SIGTERM. Nothing is pushed from a pull request. On a
push to `main` or `develop`, `release.yml` calls the same workflow after the
unit and e2e tests passed, and it pushes the release's tag: `:<sha8>` (the
first 8 characters of the commit) plus `:latest` from `main`,
`:testnet-<sha8>` from `develop` — the same build, the backend reads
everything network-specific at run time. See the root README for the release
flow.

`ui.Dockerfile` builds `ghcr.io/bze-alphateam/bze-scan-ui`. The build
context is the repository root too, trimmed by `ui.Dockerfile.dockerignore`
(BuildKit reads it instead of the root `.dockerignore`) to `ui/` without
`node_modules`, build output, `.env` files or test output:

```
docker build -f docker/ui.Dockerfile --secret id=build_env,src=ui/.env.dist --build-arg GIT_SHA=$(git rev-parse --short=8 HEAD) -t bze-scan-ui .
```

- Build env: the BuildKit secret `build_env`, mounted as `ui/.env.production`
  for `next build` only. Its `NEXT_PUBLIC_*` values are inlined into the
  browser bundle, so they are public by definition; the secret mount keeps the
  file out of the context, the layers and the log, and the copy Next traces
  into the standalone output is deleted. The image holds no other value:
  server-only settings (`API_URL`) come from the container environment.
- Build stage: `node:24-alpine`, corepack's pnpm, `pnpm install
  --frozen-lockfile`, `next build` with `output: "standalone"`.
- Runtime stage: `node:24-alpine` with the standalone server, `.next/static`
  and `public/`, `NODE_ENV=production`, `HOSTNAME=0.0.0.0`, `PORT=3000`, user
  `node`, port 3000 exposed, `node server.js` under `tini`. Next drains and
  exits with 143 on SIGTERM; `tini -e 143` reports that as 0.
- OCI labels `source`, `revision` (the sha), `description`, `licenses`.

CI (`.github/workflows/ui-image.yml`) builds it on every pull request touching
`ui/` or these files, with `ui/.env.dist` as the build env, and runs
`ui-smoke-test.sh`: the revision label must be the commit, `GET /` must answer
200 within 15 s and the container must stop with exit code 0 within 10 s of
SIGTERM. On a push to `main` or `develop`, `release.yml` calls it after
`ui-unit` and `ui-e2e` passed, with the release's tag and deploy target: it
takes that target's build env, `<deploy dir>/build.env` of the deploy
repository (sparse checkout with the `DEPLOY_REPO_TOKEN` organisation secret,
on release runs only, so pull requests never see it; the file is checked for
`NEXT_PUBLIC_` lines, never printed), builds, smoke-tests and pushes
`:testnet-<sha8>` (develop) or `:<sha8>` and `:latest` (main). Every release
image is deployed, so a release run fails without its build env.
